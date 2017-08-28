package images

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/containers/image/copy"
	"github.com/containers/image/signature"
	"github.com/containers/image/transports/alltransports"
	"github.com/containers/image/types"
	"github.com/mgoltzsche/cntnr/log"
	specs "github.com/opencontainers/image-spec/specs-go/v1"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

var toIdRegexp = regexp.MustCompile("[^a-z0-9]+")

func NewImages(imageStoreDir string, pullPolicy PullPolicy, ctx *types.SystemContext, debug log.Logger) (*Images, error) {
	imageStoreDir, err := filepath.Abs(imageStoreDir)
	if err != nil {
		return nil, fmt.Errorf("Invalid image store dir provided: %s", err)
	}
	trustPolicy, err := createTrustPolicyContext()
	if err != nil {
		return nil, fmt.Errorf("Error loading trust policy: %v", err)
	}
	return &Images{map[string]*Image{}, imageStoreDir, trustPolicy, pullPolicy, ctx, debug}, nil
}

func (self *Images) Image(name string) (*Image, error) {
	return self.fetchImage(name, self.pullPolicy)
}

func (self *Images) fetchImage(name string, pullPolicy PullPolicy) (r *Image, err error) {
	// TODO: use pull policy
	r = self.images[name]
	if r != nil {
		return
	}
	imgDir := self.toImageDirectory(name)
	// Try to load image from local store
	r, err = readImageConfig(name, imgDir)
	if err == nil {
		self.images[name] = r
		return
	} else if pullPolicy == PULL_NEVER {
		return nil, fmt.Errorf("Cannot find image %q locally: %v", name, err)
	}
	// Import image
	self.debug.Printf("Fetching image %q...", name)
	err = os.MkdirAll(imgDir, 0770)
	if err != nil {
		return nil, fmt.Errorf("Cannot create image directory: %v", err)
	}
	err = self.copyImage(name, "oci:"+imgDir)
	if err != nil {
		return nil, fmt.Errorf("Cannot fetch image: %v", err)
	}
	r, err = readImageConfig(name, imgDir)
	if err != nil {
		return nil, fmt.Errorf("Cannot read %q image config: %v", name, err)
	}
	r.Directory = imgDir
	self.images[name] = r
	return
}

func (self *Images) toImageDirectory(imgName string) string {
	// TODO: split into transport and path
	var buf bytes.Buffer
	encoder := base64.NewEncoder(base64.RawStdEncoding, &buf)
	encoder.Write([]byte(imgName))
	encoder.Close()
	return filepath.Join(self.imageDirectory, buf.String())
}

func readImageConfig(name, imgDir string) (*Image, error) {
	idx := &specs.Index{}
	err := unmarshalJSON(filepath.Join(imgDir, "index.json"), idx)
	if err != nil {
		return nil, fmt.Errorf("Cannot read OCI image index: %v", err)
	}
	for _, ref := range idx.Manifests {
		if ref.Platform.Architecture != runtime.GOARCH || ref.Platform.OS != runtime.GOOS {
			continue
		}
		d := ref.Digest
		manifestFile := filepath.Join(imgDir, "blobs", string(d.Algorithm()), d.Hex())
		manifest := &specs.Manifest{}
		if err = unmarshalJSON(manifestFile, &manifest); err != nil {
			return nil, fmt.Errorf("Cannot read OCI image manifest: %v", err)
		}
		d = manifest.Config.Digest
		configFile := filepath.Join(imgDir, "blobs", string(d.Algorithm()), d.Hex())
		config := &specs.Image{}
		if err = unmarshalJSON(configFile, config); err != nil {
			return nil, fmt.Errorf("Cannot read OCI image config: %v", err)
		}
		return &Image{name, imgDir, idx, manifest, config}, nil
	}
	return nil, fmt.Errorf("No image manifest for platform architecture %s and OS %s found in %q!", runtime.GOARCH, runtime.GOOS, imgDir)
}

func unmarshalJSON(file string, dest interface{}) error {
	b, err := ioutil.ReadFile(file)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dest)
}

func (self *Images) copyImage(src, dest string) error {
	srcRef, err := alltransports.ParseImageName(src)
	if err != nil {
		return fmt.Errorf("Invalid image source %s: %v", src, err)
	}
	destRef, err := alltransports.ParseImageName(dest)
	if err != nil {
		return fmt.Errorf("Invalid image destination %s: %v", dest, err)
	}
	return copy.Image(self.trustPolicy, destRef, srcRef, &copy.Options{
		RemoveSignatures: false,
		SignBy:           "",
		ReportWriter:     os.Stdout,
		SourceCtx:        self.context,
		DestinationCtx:   self.context,
	})
}

func createTrustPolicyContext() (*signature.PolicyContext, error) {
	policyFile := ""
	var policy *signature.Policy // This could be cached across calls, if we had an application context.
	var err error
	//if insecurePolicy {
	//	policy = &signature.Policy{Default: []signature.PolicyRequirement{signature.NewPRInsecureAcceptAnything()}}
	if policyFile == "" {
		policy, err = signature.DefaultPolicy(nil)
	} else {
		policy, err = signature.NewPolicyFromFile(policyFile)
	}
	if err != nil {
		return nil, err
	}
	return signature.NewPolicyContext(policy)
}

func (self *Images) BuildImage(uri, dockerFile, contextPath string) (*Image, error) {
	name := uri
	if len(uri) > 14 && uri[0:14] == "docker-daemon:" {
		name = uri[14:]
	}
	img, err := self.fetchImage(uri, PULL_NEVER)
	if err == nil {
		return img, nil
	}
	imgFile := filepath.FromSlash(dockerFile)
	dockerFileDir := filepath.Dir(imgFile)
	if contextPath == "" {
		contextPath = dockerFileDir
	}
	self.debug.Printf("Building docker image from %q...", imgFile)
	c := exec.Command("docker", "build", "-t", name, "--rm", dockerFileDir)
	c.Dir = contextPath
	c.Stdout = os.Stdout // TODO: write to log
	c.Stderr = os.Stderr
	if err = c.Run(); err != nil {
		return nil, err
	}
	img, err = self.fetchImage(uri, PULL_UPDATE)
	if err != nil {
		return nil, err
	}
	self.images[name] = img
	return img, nil
}

func toId(v string) string {
	return strings.Trim(toIdRegexp.ReplaceAllLiteralString(strings.ToLower(v), "-"), "-")
}

func removeFile(file string) {
	e := os.Remove(file)
	if e != nil {
		os.Stderr.WriteString(fmt.Sprintf("image loader: %s\n", e))
	}
}
