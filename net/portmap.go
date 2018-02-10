package net

import (
	"strconv"
	"strings"

	"github.com/pkg/errors"
)

// See https://github.com/containernetworking/plugins/blob/master/plugins/meta/portmap/main.go
type PortMapEntry struct {
	HostPort      uint16 `json:"hostPort"`
	ContainerPort uint16 `json:"containerPort"`
	Protocol      string `json:"protocol"`
	HostIP        string `json:"hostIP,omitempty"`
}

func (p PortMapEntry) String() string {
	var s string
	pub := p.HostPort
	if pub == 0 {
		pub = p.ContainerPort
	}
	if p.HostIP == "" {
		s = strconv.Itoa(int(pub)) + ":"
	} else {
		s = p.HostIP + ":" + strconv.Itoa(int(pub)) + ":"
	}
	s += strconv.Itoa(int(p.ContainerPort))
	if p.Protocol != "" && p.Protocol != "tcp" {
		s += "/" + p.Protocol
	}
	return s
}

func ParsePortMapping(expr string, r *[]PortMapEntry) (err error) {
	defer func() {
		err = errors.Wrapf(err, "invalid port binding expression %q", expr)
	}()

	sp := strings.Split(expr, "/")
	if len(sp) > 2 {
		return errors.New("too many '/' occurences")
	}
	prot := "tcp"
	if len(sp) == 2 {
		prot = strings.ToLower(sp[1])
		if prot == "" {
			return errors.New("no protocol defined after '/'")
		}
	}

	var hostIp, hostPortExpr, targetPortExpr string
	psi := strings.LastIndex(sp[0], ":")
	hostPart := sp[0]
	if psi > 0 && psi+1 < len(sp[0]) {
		hostPart = sp[0][:psi]
		targetPortExpr = sp[0][psi+1:]
	}
	isi := strings.LastIndex(hostPart, ":")
	hostPortExpr = hostPart
	if isi > 0 && isi+1 > len(hostPart) {
		hostIp = hostPart[:isi]
		hostPortExpr = hostPart[isi+1:]
	}
	if targetPortExpr == "" {
		targetPortExpr = hostPortExpr
	}
	hostFrom, hostTo, err := toPortRange(hostPortExpr)
	if err != nil {
		return
	}
	targetFrom, targetTo, err := toPortRange(targetPortExpr)
	if err != nil {
		return
	}
	rangeSize := targetTo - targetFrom
	if (hostTo - hostFrom) != rangeSize {
		return errors.Errorf("port %q's range size differs between host and destination", expr)
	}
	for i := 0; i <= rangeSize; i++ {
		targetPort := targetFrom + i
		pubPort := hostFrom + i
		if targetPort < 0 || targetPort > 65535 {
			return errors.Errorf("target port %d exceeds range", targetPort)
		}
		if pubPort < 0 || pubPort > 65535 {
			return errors.Errorf("published port %d exceeds range", pubPort)
		}
		*r = append(*r, PortMapEntry{uint16(targetPort), uint16(pubPort), prot, hostIp})
	}
	return nil
}

func toPortRange(rangeExpr string) (from, to int, err error) {
	s := strings.Split(rangeExpr, "-")
	if len(s) < 3 {
		from, err = strconv.Atoi(s[0])
		if err == nil {
			if len(s) == 2 {
				to, err = strconv.Atoi(s[1])
				if err == nil && from <= to {
					return
				}
			} else {
				to = from
				return
			}
		}
	}
	err = errors.Errorf("invalid port range %q", rangeExpr)
	return
}
