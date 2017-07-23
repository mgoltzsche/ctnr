package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/containers/image/types"
	"github.com/mgoltzsche/cntnr/images"
	"github.com/mgoltzsche/cntnr/log"
	"github.com/mgoltzsche/cntnr/model"
	"github.com/mgoltzsche/cntnr/net"
	"github.com/mgoltzsche/cntnr/run"
	"os"
	"os/user"
	"path/filepath"
)

var (
	// global options
	verbose      bool
	imgDir       string
	containerDir string
	rootless     bool

	// runtime vars
	errorLog = log.NewStdLogger(os.Stderr)
	warnLog  = log.NewStdLogger(os.Stderr)
	debugLog = log.NewNopLogger()
)

func usageError() {
	fmt.Fprintf(os.Stderr, "Usage: %s OPTIONS ARGUMENTS\n", os.Args[0])
	fmt.Fprint(os.Stderr, "\nArguments:\n")
	fmt.Fprintf(os.Stderr, "  run FILE\n\tRuns pod from docker-compose.yml or pod.json file\n")
	fmt.Fprintf(os.Stderr, "  image info IMAGE\n\tPrints image metadata in JSON\n")
	fmt.Fprintf(os.Stderr, "  net add NAME\n\tAdds network NET to process' current network namespace using CNI\n")
	fmt.Fprintf(os.Stderr, "  net del NAME\n\tDeletes network NET from process' current network namespace\n")
	fmt.Fprint(os.Stderr, "\nOptions:\n")
	flag.PrintDefaults()
	os.Exit(1)
}

func initFlags() {
	usr, err := user.Current()
	if err != nil {
		errorLog.Printf("Cannot get user's home directory: %v", err)
		os.Exit(2)
	}
	rootless = usr.Uid != "0"
	defaultImgDir := filepath.Join(usr.HomeDir, ".cntnr", "images")
	defaultContainerDir := filepath.Join(usr.HomeDir, ".cntnr", "containers")

	// global options
	flag.BoolVar(&verbose, "verbose", false, "enables verbose log output")
	flag.StringVar(&imgDir, "image-dir", defaultImgDir, "Directory to store images")
	flag.StringVar(&containerDir, "container-dir", defaultContainerDir, "Directory to store OCI runtime bundles")
}

func main() {
	initFlags()
	flag.Parse()

	if verbose {
		debugLog = log.NewStdLogger(os.Stderr)
	}

	var err error

	switch flag.Arg(0) {
	case "run":
		if flag.NArg() != 2 {
			usageError()
		}
		err = runCompose(flag.Arg(1))
	case "image":
		switch flag.Arg(1) {
		case "info":
			if flag.NArg() != 3 {
				usageError()
			}
			err = printImageConfig(flag.Arg(2))
		default:
			errorLog.Printf("Invalid argument %q", flag.Arg(1))
			usageError()
		}
	case "net":
		switch flag.Arg(1) {
		case "add":
			if flag.NArg() < 3 {
				usageError()
				os.Exit(1)
			}
			err = net.AddNet(flag.Args()[2:])
		case "del":
			if flag.NArg() < 3 {
				usageError()
				os.Exit(1)
			}
			err = net.DelNet(flag.Args()[2:])
		default:
			errorLog.Printf("Invalid argument %q", flag.Arg(1))
			usageError()
		}
	default:
		usageError()
	}

	if err != nil {
		errorLog.Println(err)
		os.Exit(2)
	}
}

func runCompose(file string) error {
	project, err := model.LoadProject(file, "./volumes", warnLog)
	if err != nil {
		return fmt.Errorf("Could not load project: %v", err)
	}
	imgs, err := newImages()
	if err != nil {
		return err
	}
	// TODO: provide cli option
	rootDir := "/run/runc"
	if rootless {
		rootDir = "/tmp/runc"
	}
	manager := run.NewContainerManager(debugLog)
	for _, s := range project.Services {
		containerId := s.Name
		b, err := createRuntimeBundle(&s, imgs, containerId, filepath.Join(containerDir, containerId))
		if err != nil {
			return err
		}

		c, err := run.NewContainer(containerId, b.Dir, rootDir, b.Spec, s.StdinOpen, errorLog, debugLog)
		if err != nil {
			return err
		}

		err = manager.Deploy(c)
		if err != nil {
			manager.Stop()
			return err
		}
	}

	manager.HandleSignals()
	err = manager.Wait()
	return err
}

func printImageConfig(imgName string) error {
	imgs, err := newImages()
	if err != nil {
		return err
	}
	img, err := imgs.Image(imgName)
	if err != nil {
		return err
	}
	os.Stdout.WriteString(toJSON(img.Config) + "\n")
	return nil
}

func newImages() (*images.Images, error) {
	imgCtx := imageContext()
	imgs, err := images.NewImages(imgDir, images.PULL_NEW, imgCtx, debugLog)
	if err != nil {
		return nil, fmt.Errorf("Could not init images: %v", err)
	}
	return imgs, nil
}

func imageContext() *types.SystemContext {
	// TODO: provide CLI options
	c := &types.SystemContext{
		RegistriesDirPath:           "",
		DockerCertPath:              "",
		DockerInsecureSkipTLSVerify: true,
		OSTreeTmpDirPath:            "ostree-tmp-dir",
		// TODO: add docker auth
		//DockerAuthConfig: dockerAuth,
	}

	if rootless {
		c.DockerCertPath = "./docker-cert"
	}

	return c
}

func createRuntimeBundle(s *model.Service, imgs *images.Images, id, dir string) (*model.RuntimeBundleBuilder, error) {
	b, err := s.NewRuntimeBundleBuilder(id, dir, imgs, rootless)
	if err != nil {
		return nil, err
	}
	if err := b.Build(debugLog); err != nil {
		return nil, err
	}
	return b, nil
}

func toJSON(o interface{}) string {
	b, err := json.MarshalIndent(o, "", "\t")
	if err != nil {
		panic(err.Error())
	}
	return string(b)
}
