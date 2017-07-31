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
	hostsEntries  = HostsMap(map[string]string{})
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
	fmt.Fprintf(os.Stderr, "  run FILE\n\tRuns pod from docker-compose.yml or pod.json file\n")
	fmt.Fprintf(os.Stderr, "  image info IMAGE\n\tPrints image metadata in JSON\n")
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

type HostsMap map[string]string

func (m *HostsMap) Set(v string) error {
	s := strings.SplitN(v, "=", 2)
	ip := strings.Trim(s[0], " ")
	if len(s) != 2 || ip == "" || strings.Trim(s[1], " ") == "" {
		return fmt.Errorf("Invalid hosts entry argument: %q. Expected format: IP=NAME", v)
	}
	(*m)[ip] = (*m)[ip] + " " + strings.Trim(s[1], " ")
	return nil
}

func (m *HostsMap) String() string {
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

func parseNetworkFlags(f *flag.FlagSet, args []string) error {
	f.StringVar(&interfaceName, "pub-if", "", "Network interface to map hostname to. Default is eth0 when network added else lo")
	f.StringVar(&hostname, "hostname", "", "hostname as written to /etc/hostname and /etc/hosts")
	f.Var(&hostsEntries, "hosts-entry", "Entries in form of IP=NAME that are written to /etc/hosts")
	f.Var(&dnsNameserver, "dns", "List of DNS nameservers to write in /etc/resolv.conf")
	f.Var(&dnsSearch, "dns-search", "List of DNS search domains to write in /etc/resolv.conf")
	f.Var(&dnsOptions, "dns-opt", "List of DNS options to write in /etc/resolv.conf")
	f.StringVar(&dnsDomain, "dns-domain", "", "DNS domain")
	return f.Parse(args)
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
		case "init":
			fs := flag.NewFlagSet("init", flag.ContinueOnError)
			err = parseNetworkFlags(fs, flag.Args()[2:])
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
			netMan.AddHostsEntries(hostsEntries)
			err = netMan.AddHostnameHostsEntry(pubIf)
			if err != nil {
				break
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

	// Umarshal the hook state
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
