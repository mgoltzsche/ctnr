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

func initFlags() {
	usr, err := user.Current()
	if err != nil {
		errorLog.Printf("Cannot get user's home directory: %v", err)
		os.Exit(1)
	}
	rootless = usr.Uid != "0"
	defaultImgDir := filepath.Join(usr.HomeDir, ".cntnr", "images")
	defaultContainerDir := filepath.Join(usr.HomeDir, ".cntnr", "containers")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s OPTIONS ARGUMENTS\n", os.Args[0])
		fmt.Fprint(os.Stderr, "\nArguments:\n")
		fmt.Fprintf(os.Stderr, "  run FILE\n\tRuns pod from docker-compose.yml or pod.json file\n")
		fmt.Fprintf(os.Stderr, "  image config IMAGE\n\tPrints image metadata in JSON\n")
		fmt.Fprintf(os.Stderr, "  net create CONTAINERID NET\n\tCreates a new container namespace CONTAINERID with network NET using CNI\n")
		fmt.Fprintf(os.Stderr, "  net delete CONTAINERID\n\tDeletes the network namespace\n")
		fmt.Fprint(os.Stderr, "\nOptions:\n")
		flag.PrintDefaults()
	}

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
			flag.Usage()
			os.Exit(1)
		}
		err = run(flag.Arg(1))
	case "image":
		switch flag.Arg(1) {
		case "config":
			if flag.NArg() != 3 {
				flag.Usage()
				os.Exit(1)
			}
			err = printImageConfig(flag.Arg(2))
		default:
			errorLog.Printf("Invalid argument %q", flag.Arg(1))
			os.Exit(1)
		}
	case "net":
		switch flag.Arg(1) {
		case "create":
			if flag.NArg() != 4 {
				flag.Usage()
				os.Exit(1)
			}
			err = net.CreateNetNS(flag.Arg(2), flag.Arg(3))
		case "delete":
			if flag.NArg() != 3 {
				flag.Usage()
				os.Exit(1)
			}
			err = net.DeleteNetNS(flag.Arg(2))
		default:
			errorLog.Printf("Invalid argument %q", flag.Arg(1))
			os.Exit(1)
		}
	default:
		errorLog.Printf("Invalid argument %q", flag.Arg(0))
		os.Exit(1)
	}

	if err != nil {
		errorLog.Println(err)
		os.Exit(2)
	}
}

func run(file string) error {
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
		b, err := createRuntimeBundle(&s, imgs, filepath.Join(containerDir, containerId))
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

func createRuntimeBundle(s *model.Service, imgs *images.Images, dir string) (*model.RuntimeBundleBuilder, error) {
	b, err := s.NewRuntimeBundleBuilder(dir, imgs, rootless)
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
