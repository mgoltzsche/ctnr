package model

import (
	"encoding/json"
	"fmt"
	//"github.com/mgoltzsche/cntnr/log"
	"github.com/mgoltzsche/cntnr/images"
	//"github.com/opencontainers/runc/libcontainer/specconv"
	"github.com/mgoltzsche/cntnr/libcontainer/specconv"
	//"github.com/opencontainers/image-tools/image"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"io/ioutil"
	"path/filepath"
	"strings"
	"time"
)

func loadImage(service *Service, imgs *images.Images) (*images.Image, error) {
	// TODO: build image
	if service.Image == "" {
		return nil, fmt.Errorf("Service %q has no image", service.Name)
	}
	img, err := imgs.Image(service.Image)
	if err != nil {
		return nil, err
	}
	return img, nil
}

func CreateRuntimeBundle(containerDir string, service *Service, imgs *images.Images, rootless bool) error {
	if service.Name == "" {
		return fmt.Errorf("Service has no name")
	}
	img, err := loadImage(service, imgs)
	if err != nil {
		return err
	}
	if img == nil {
		return fmt.Errorf("Service %q has no loaded image", service.Name)
	}
	//err = image.UnpackLayout(img.Directory, containerDir, "latest")
	//err = image.CreateRuntimeBundleLayout(img.Directory, containerDir, "latest", "rootfs")
	err = img.Unpack(containerDir)
	if err != nil {
		return fmt.Errorf("Unpacking OCI layout of image %q (%s) failed: %v", service.Image, img.Directory, err)
	}
	spec, err := transform(service, img, rootless)
	b, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return fmt.Errorf("Cannot unmarshal OCI runtime spec for service %q in %s: %v", service.Name, containerDir, err)
	}
	err = ioutil.WriteFile(filepath.Join(containerDir, "config.json"), b, 0770)
	if err != nil {
		return fmt.Errorf("Cannot write OCI runtime spec for service %q in %s: %v", service.Name, containerDir, err)
	}
	return nil
}

func transform(service *Service, img *images.Image, rootless bool) (*specs.Spec, error) {
	spec := specconv.Example()

	err := applyService(img, service, spec)
	if err != nil {
		return nil, err
	}

	if rootless {
		specconv.ToRootless(spec)
	}

	return spec, nil
}

func applyService(img *images.Image, service *Service, spec *specs.Spec) error {
	if service.Hostname == "" {
		// TODO: set container ID as hostname
	} else {
		spec.Hostname = service.Hostname
	}
	// TODO: domainname must be written into container's resolv.conf

	// Apply args
	imgCfg := img.Config.Config
	entrypoint := imgCfg.Entrypoint
	cmd := imgCfg.Cmd
	if entrypoint == nil {
		entrypoint = []string{}
	}
	if cmd == nil {
		cmd = []string{}
	}
	if service.Entrypoint != nil {
		entrypoint = append(entrypoint, service.Entrypoint...)
	}
	if service.Command != nil {
		cmd = append(cmd, service.Command...)
	}
	spec.Process.Args = append(entrypoint, cmd...)

	// Apply env
	env := map[string]string{}
	for _, e := range imgCfg.Env {
		kv := strings.SplitN(e, "=", 2)
		k := kv[0]
		v := ""
		if len(kv) == 2 {
			v = kv[1]
		}
		env[k] = v
	}
	for k, v := range service.Environment {
		env[k] = fmt.Sprintf("%q", v)
	}
	spec.Process.Env = make([]string, len(env))
	i := 0
	for k, v := range env {
		spec.Process.Env[i] = k + "=" + v
		i++
	}

	// Apply cwd
	spec.Process.Cwd = imgCfg.WorkingDir
	if service.Cwd != "" {
		spec.Process.Cwd = service.Cwd
	}

	spec.Process.Terminal = service.Tty

	// Apply annotations
	spec.Annotations = map[string]string{}
	// TODO: extract annotations from image index and manifest
	if img.Config.Author != "" {
		spec.Annotations["org.opencontainers.image.author"] = img.Config.Author
	}
	if !time.Unix(0, 0).Equal(*img.Config.Created) {
		spec.Annotations["org.opencontainers.image.created"] = img.Config.Created.String()
	}
	/* TODO: enable if supported:
	if img.StopSignal != "" {
		spec.Annotations["org.opencontainers.image.stopSignal"] = img.Config.StopSignal
	}*/
	if imgCfg.Labels != nil {
		for k, v := range imgCfg.Labels {
			spec.Annotations[k] = v
		}
	}
	if service.StopSignal != "" {
		spec.Annotations["org.opencontainers.image.stopSignal"] = service.StopSignal
	}

	// Add exposed ports
	expose := map[string]bool{}
	if imgCfg.ExposedPorts != nil {
		for k := range imgCfg.ExposedPorts {
			expose[k] = true
		}
	}
	if service.Expose != nil {
		for _, e := range service.Expose {
			expose[e] = true
		}
	}
	if len(expose) > 0 {
		exposecsv := make([]string, len(expose))
		i := 0
		for k := range expose {
			exposecsv[i] = k
			i++
		}
		spec.Annotations["org.opencontainers.image.exposedPorts"] = strings.Join(exposecsv, ",")
	}

	// TODO: apply user (username must be parsed from rootfs/etc/passwd and mapped to uid/gid)

	// TODO: mount separate paths in /proc/self/fd to apply service.StdinOpen
	spec.Root.Readonly = service.ReadOnly

	// TODO: mount volumes

	// TODO: register healthcheck (as Hook)
	// TODO: bind ports (propably in networking Hook)

	return nil
}
