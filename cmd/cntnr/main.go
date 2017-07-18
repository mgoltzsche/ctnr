package main

import (
	"flag"
	"fmt"
	//"github.com/mgoltzsche/cntnr/containers"
	"github.com/mgoltzsche/cntnr/images"
	"github.com/mgoltzsche/cntnr/log"
	"github.com/mgoltzsche/cntnr/model"
	"os"
	"os/user"
	"path/filepath"
)

var (
	// global options
	verbose      bool
	imgDir       string
	containerDir string

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
	defaultImgDir := filepath.Join(usr.HomeDir, ".cntnr", "images")
	defaultContainerDir := filepath.Join(usr.HomeDir, ".cntnr", "containers")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s OPTIONS ARGUMENTS\n", os.Args[0])
		fmt.Fprint(os.Stderr, "\nArguments:\n")
		fmt.Fprintf(os.Stderr, "  run FILE\n\tRuns pod from docker-compose.yml or pod.json file\n")
		fmt.Fprintf(os.Stderr, "  json FILE\n\tPrints pod model from file as JSON\n")
		fmt.Fprint(os.Stderr, "\nOptions:\n")
		flag.PrintDefaults()
	}

	// global options
	flag.BoolVar(&verbose, "verbose", false, "enables verbose log output")
	flag.StringVar(&imgDir, "image-dir", defaultImgDir, "Directory to store images")
	flag.StringVar(&containerDir, "container-dir", defaultContainerDir, "Directory to store containers")
}

func main() {
	initFlags()
	flag.Parse()

	if verbose {
		debugLog = log.NewStdLogger(os.Stderr)
	}

	if flag.NArg() != 2 {
		flag.Usage()
		os.Exit(1)
	}

	var err error

	switch flag.Arg(0) {
	case "run":
		err = run(flag.Arg(1))
	case "json":
		err = dumpJSON(flag.Arg(1))
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
	imgs, err := images.NewImages(imgDir, images.PULL_NEW, debugLog)
	if err != nil {
		return fmt.Errorf("Could not init images: %v", err)
	}
	for _, s := range project.Services {
		// TODO: check if not working on a copy
		err := model.CreateRuntimeBundle(filepath.Join(containerDir, s.Name), &s, imgs, true)
		if err != nil {
			return err
		}
		/*c, err := containers.NewContainer(filepath.Join(defaultContainerDir, id), img, spec, time.Duration(10000000000), errorLog, debugLog)
		if err != nil {
			return err
		}
		c.Start()*/
		//fmt.Println(spec)
	}
	//fmt.Println(project.JSON())
	return err
}

func dumpJSON(file string) error {
	return nil
}
