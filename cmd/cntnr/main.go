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
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"io/ioutil"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

var (
	// global options
	verbose      bool
	imgDir       string
	containerDir string
	rootless     bool

	// network options
	hostname      string
	interfaceName string
	hostsEntries  = KVList([][2]string{})
	dnsNameserver = StringList([]string{})
	dnsSearch     = StringList([]string{})
	dnsOptions    = StringList([]string{})
	dnsDomain     string

	// runtime vars
	errorLog = log.NewStdLogger(os.Stderr)
	warnLog  = log.NewStdLogger(os.Stderr)
	debugLog = log.NewNopLogger()
)

func usageError() {
	fmt.Fprintf(os.Stderr, "Usage: %s OPTIONS ARGUMENTS\n", os.Args[0])
	fmt.Fprint(os.Stderr, "\nArguments:\n")
	//fmt.Fprintf(os.Stderr, "  ls\n\tLists all containers\n")
	//fmt.Fprintf(os.Stderr, "  state CONTAINERID\n\tPrints the container's state\n")
	//fmt.Fprintf(os.Stderr, "  bundle IMAGE [OPTIONS]\n\tCreates a new runtime bundle\n") // --bundle=BUNDLEDIR --no-create
	//fmt.Fprintf(os.Stderr, "  create CONTAINERID\n\tCreates a new runtime bundle\n") // --bundle=BUNDLEDIR
	//fmt.Fprintf(os.Stderr, "  run IMAGE [OPTIONS] [ARGS] [--- IMAGE [OPTIONS] [ARGS]...]\n\tCreates and runs one or multiple runtime bundles\n") // ATTENTION: must make sure that options
	//fmt.Fprintf(os.Stderr, "  run-container CONTAINERID\n\tCreates and runs one or multiple runtime bundles\n")
	fmt.Fprintf(os.Stderr, "  run-compose FILE\n\tRuns all services from a docker-compose.yml FILE\n")
	//fmt.Fprintf(os.Stderr, "  image ls\n\tLists all imported images\n")
	//fmt.Fprintf(os.Stderr, "  image build NAME [FILE]\n\tBuilds an image\n")
	//fmt.Fprintf(os.Stderr, "  image export\n\tLists all imported images\n")
	fmt.Fprintf(os.Stderr, "  image import URI\n\tImports an image from a given URI as e.g. docker://alpine:latest\n")
	fmt.Fprintf(os.Stderr, "  image info URI\n\tPrints image metadata in JSON\n")
	fmt.Fprintf(os.Stderr, "  net init [NAME1 [NAME2]]\n\tAdds networks to process' current network namespace using CNI and writes container's /etc/hostname, /etc/hosts and /etc/resolv.conf\n")
	fmt.Fprintf(os.Stderr, "  net del [NAME1 [NAME2]]\n\tDeletes network NET from process' current network namespace\n")
	fmt.Fprint(os.Stderr, "\nOptions:\n")
	flag.PrintDefaults()
	os.Exit(1)
}

type StringList []string

func (l *StringList) Set(v string) error {
	*l = append([]string(*l), v)
	return nil
}

func (l *StringList) String() string {
	return strings.Join([]string(*l), " ")
}

type KVList [][2]string

func (m *KVList) Set(v string) error {
	s := strings.SplitN(v, "=", 2)
	k := strings.Trim(s[0], " ")
	if len(s) != 2 || k == "" || strings.Trim(s[1], " ") == "" {
		return fmt.Errorf("Invalid KV argument: %q. Expected format: KEY=VALUE", v)
	}
	(*m) = append((*m), [2]string{k, strings.Trim(s[1], " ")})
	return nil
}

func (m *KVList) String() string {
	s := ""
	for ip, name := range *m {
		s += fmt.Sprintf("%-15s  %s\n", ip, name)
	}
	return s
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

func networkFlags(f *flag.FlagSet) {
	f.StringVar(&interfaceName, "pub-if", "", "Network interface to map hostname to. Default is eth0 when network added else lo")
	f.StringVar(&hostname, "hostname", "", "hostname as written to /etc/hostname and /etc/hosts")
	f.Var(&hostsEntries, "hosts-entry", "Entries in form of IP=NAME that are written to /etc/hosts")
	f.Var(&dnsNameserver, "dns", "List of DNS nameservers to write in /etc/resolv.conf")
	f.Var(&dnsSearch, "dns-search", "List of DNS search domains to write in /etc/resolv.conf")
	f.Var(&dnsOptions, "dns-opt", "List of DNS options to write in /etc/resolv.conf")
	f.StringVar(&dnsDomain, "dns-domain", "", "DNS domain")
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
		/*fs := flag.NewFlagSet("run", flag.ContinueOnError)
		runFlags(fs)
		err = fs.Parse(flag.Args()[2:])
		if err != nil {
			break
		}*/
		if flag.NArg() != 2 {
			usageError()
		}
		// TODO: use urfave/cli to handle subcommands and multiple input sources
		err = runCompose(flag.Arg(1))
	case "image":
		switch flag.Arg(1) {
		case "import":
			if flag.NArg() != 3 {
				usageError()
			}
			_, err = loadImage(flag.Arg(2))
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
		case "init":
			fs := flag.NewFlagSet("init", flag.ContinueOnError)
			networkFlags(fs)
			err = fs.Parse(flag.Args()[2:])
			if err != nil {
				break
			}
			netMan := containerNetworkManager()
			pubIf := interfaceName
			if pubIf == "" {
				if len(fs.Args()) > 0 {
					pubIf = "eth0"
				} else {
					pubIf = "lo"
				}
			}
			for i, n := range fs.Args() {
				if _, err = netMan.AddNet("eth"+strconv.Itoa(i), n); err != nil {
					break
				}
			}
			netMan.SetHostname(hostname)
			err = netMan.AddHostnameHostsEntry(pubIf)
			if err != nil {
				break
			}
			for _, kv := range hostsEntries {
				netMan.AddHostsEntry(kv[0], kv[1])
			}
			netMan.AddDNS(net.DNS{
				dnsNameserver,
				dnsSearch,
				dnsOptions,
				dnsDomain,
			})
			err = netMan.Apply()
		case "del":
			if flag.NArg() < 3 {
				usageError()
				os.Exit(1)
			}
			// TODO: Make sure all network resources are removed properly since currently IP/interface stay reserved
			netMan := containerNetworkManager()
			for i, n := range flag.Args()[2:] {
				if e := netMan.DelNet("eth"+strconv.Itoa(i), n); e != nil && err == nil {
					err = e
				}
			}
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

func parseHostMapping(a []string) (map[string]string, error) {
	m := map[string]string{}
	for _, e := range a {
		s := strings.SplitN(e, "=", 2)
		ip := strings.Trim(s[0], " ")
		if len(s) != 2 || ip == "" || strings.Trim(s[1], " ") == "" {
			return m, fmt.Errorf("Invalid hosts entry argument: %q. Expected format: IP=NAME", e)
		}
		m[ip] = strings.Trim(s[1], " ") + " " + m[ip]
	}
	return m, nil
}

func containerNetworkManager() *net.ContainerNetManager {
	m, err := net.NewContainerNetManager(readContainerState())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot create container network manager: %s", err)
		os.Exit(1)
	}
	return m
}

func readContainerState() *specs.State {
	state := &specs.State{}
	// Read hook data from stdin
	b, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot read OCI state from stdin: %v\n", err)
		os.Exit(1)
	}

	// Unmarshal the hook state
	if err := json.Unmarshal(b, state); err != nil {
		fmt.Fprintf(os.Stderr, "Cannot unmarshal OCI state from stdin: %v\n", err)
		os.Exit(1)
	}

	return state
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
		bundleDir := filepath.Join(containerDir, containerId)
		vols := model.NewVolumeResolver(project, bundleDir)
		b, err := createRuntimeBundle(&s, imgs, vols, containerId, bundleDir)
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

func loadImage(imgName string) (*images.Image, error) {
	imgs, err := newImages()
	if err != nil {
		return nil, err
	}
	return imgs.Image(imgName)
}

func printImageConfig(imgName string) error {
	img, err := loadImage(imgName)
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

func createRuntimeBundle(s *model.Service, imgs *images.Images, vols model.VolumeResolver, id, dir string) (*model.RuntimeBundleBuilder, error) {
	b, err := s.NewRuntimeBundleBuilder(id, dir, imgs, vols, rootless)
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
