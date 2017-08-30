package images

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/containers/image/copy"
	"github.com/containers/image/signature"
	"github.com/containers/image/transports/alltransports"
	"github.com/containers/image/types"
	"github.com/mgoltzsche/cntnr/log"
	digest "github.com/opencontainers/go-digest"
	specs "github.com/opencontainers/image-spec/specs-go/v1"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

type PullPolicy string

const (
	PULL_NEVER  PullPolicy = "never"
	PULL_NEW    PullPolicy = "new"
	PULL_UPDATE PullPolicy = "update"
)

var toIdRegexp = regexp.MustCompile("[^a-z0-9]+")

type Images struct {
	images      map[string]*Image
	dir         string
	trustPolicy *signature.PolicyContext
	pullPolicy  PullPolicy
	context     *types.SystemContext
	debug       log.Logger
}

func NewImages(imageStoreDir string, pullPolicy PullPolicy, ctx *types.SystemContext, debug log.Logger) (*Images, error) {
	imageStoreDir, err := filepath.Abs(imageStoreDir)
	if err != nil {
		return nil, fmt.Errorf("Invalid image store dir provided: %s", err)
	}
	trustPolicy, err := createTrustPolicyContext()
	if err != nil {
		return nil, fmt.Errorf("Error loading trust policy: %s", err)
	}
	return &Images{map[string]*Image{}, imageStoreDir, trustPolicy, pullPolicy, ctx, debug}, nil
}

func (self *Images) Image(src string) (*Image, error) {
	return self.fetchImage(src, self.pullPolicy)
}

func (self *Images) List() (r []*Image, err error) {
	refDir := filepath.Join(self.dir, "refs")
	fs, err := ioutil.ReadDir(refDir)
	if err != nil {
		return
	}
	r = make([]*Image, len(fs))
	for i, f := range fs {
		// TODO: add creation date and size
		/*		s, e := os.Stat(filepath.Join(refDir, f.Name()))
				if e != nil {
					return nil, e
				}*/
		img, err := self.LoadImage(self.refName(f.Name()))
		if err != nil {
			return nil, err
		}
		r[i] = img
	}
	return
}

func (self *Images) fetchImage(name string, pullPolicy PullPolicy) (img *Image, err error) {
	// Try to load image from local store
	if img, err = self.LoadImage(name); err == nil {
		return
	} else if pullPolicy == PULL_NEVER {
		return nil, fmt.Errorf("Cannot find image %q in the local store: %s", name, err)
	}

	// Import image
	// TODO: handle update pull policy
	blobDir := filepath.Join(self.dir, "blobs")
	if err = os.MkdirAll(blobDir, 0770); err != nil {
		return
	}
	tmpDir := filepath.Join(self.dir, "tmp")
	if err = os.MkdirAll(tmpDir, 0770); err != nil {
		return
	}
	tmpImgDir, err := ioutil.TempDir(tmpDir, "image-")
	if err != nil {
		return
	}
	defer os.RemoveAll(tmpImgDir)
	if err = os.Symlink(blobDir, filepath.Join(tmpImgDir, "blobs")); err != nil {
		return
	}
	self.debug.Printf("Fetching image %q...", name)
	if err != nil {
		return nil, fmt.Errorf("Cannot create image directory: %s", err)
	}
	err = self.copyImage(name, "oci:"+tmpImgDir)
	if err != nil {
		return nil, fmt.Errorf("Cannot fetch image: %s", err)
	}
	manifestDigest, err := findManifestDigest(tmpImgDir)
	if err != nil {
		return
	}
	if err = self.setImageRef(name, manifestDigest); err != nil {
		return
	}
	return self.LoadImage(name)
}

func (self *Images) LoadImage(name string) (img *Image, err error) {
	if img = self.images[name]; img != nil {
		return
	}

	refFile := self.refFile(name)
	f, err := os.Lstat(refFile)
	if err != nil {
		if os.IsNotExist(err) {
			err = fmt.Errorf("Cannot find image ref %q in local store (%s)", name, self.dir)
		} else {
			err = fmt.Errorf("Cannot access image ref %q: %s", name, err)
		}
		return
	}
	if f.Mode()&os.ModeSymlink == 0 {
		return nil, fmt.Errorf("image ref file %s is not a symlink", refFile)
	}
	manifestFile, err := os.Readlink(refFile)
	if err != nil {
		return
	}
	alg := filepath.Base(filepath.Dir(manifestFile))
	hash := filepath.Base(manifestFile)
	img = &Image{name, digest.NewDigestFromHex(alg, hash), f.ModTime(), self.dir, nil, nil}
	self.images[name] = img
	return
}

func (self *Images) setImageRef(name string, manifest digest.Digest) (err error) {
	refFile := self.refFile(name)
	if err = os.MkdirAll(filepath.Dir(refFile), 0770); err != nil {
		return
	}
	manifestFile := blobFile(self.dir, manifest)
	if _, err = os.Stat(manifestFile); err != nil {
		return fmt.Errorf("Cannot set image ref %q since manifest file cannot be resolved: %s", err)
	}
	manifestFile, err = filepath.Rel(filepath.Dir(refFile), manifestFile)
	if err != nil {
		panic("Cannot create relative manifest blob file path: " + err.Error())
	}
	return os.Symlink(manifestFile, refFile)
}

func (self *Images) refFile(name string) string {
	name = base64.RawStdEncoding.EncodeToString([]byte(name))
	return filepath.Join(self.dir, "refs", name)
}

func (self *Images) refName(refFile string) string {
	name, err := base64.RawStdEncoding.DecodeString(refFile)
	if err != nil {
		panic(fmt.Sprintf("Unsupported image ref file name %q: %s\n", refFile, err))
	}
	return string(name)
}

func findManifestDigest(imgDir string) (d digest.Digest, err error) {
	idx := &specs.Index{}
	if err = unmarshalJSON(filepath.Join(imgDir, "index.json"), idx); err != nil {
		err = fmt.Errorf("Cannot read image index: %s", err)
		return
	}

	for _, ref := range idx.Manifests {
		if ref.Platform.Architecture == runtime.GOARCH && ref.Platform.OS == runtime.GOOS {
			return ref.Digest, nil
		}
	}
	err = fmt.Errorf("No image manifest for platform architecture %s and OS %s found in %q!", runtime.GOARCH, runtime.GOOS, imgDir)
	return
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
		return fmt.Errorf("Invalid image source %s: %s", src, err)
	}
	destRef, err := alltransports.ParseImageName(dest)
	if err != nil {
		return fmt.Errorf("Invalid image destination %s: %s", dest, err)
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
