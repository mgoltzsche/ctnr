package dockerfile

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mgoltzsche/cntnr/pkg/idutils"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestDockerfileApply(t *testing.T) {
	files, err := filepath.Glob("testfiles/*.test")
	require.NoError(t, err)
Files:
	for _, file := range files {
		fmt.Println("CASE ", file)
		dfile, err := os.Open(file)
		require.NoError(t, err)
		defer dfile.Close()
		efile, err := os.Open(file[0:len(file)-4] + "expected")
		require.NoError(t, err)
		defer efile.Close()
		b, err := ioutil.ReadAll(efile)
		expected := strings.TrimSpace(string(b))
		expectedLinesBr := strings.Split(expected, "\n")
		expectedLines := make([]string, 0, len(expectedLinesBr))
		for _, eline := range expectedLinesBr {
			if eline != "" {
				expectedLines = append(expectedLines, eline)
			}
		}
		require.NoError(t, err)
		args := map[string]string{
			"argp": "pval",
		}
		testee, err := LoadDockerfile(dfile, "./ctx", args, log.New(os.Stderr, "warn: "+file+":", 0))
		err = errors.WithMessage(err, file)
		require.NoError(t, err)
		mock := mockBuilder{}
		err = testee.Apply(&mock)
		require.NoError(t, err)
		for i, eline := range expectedLines {
			aline := ""
			if len(mock.ops) > i {
				aline = mock.ops[i]
			}
			if eline != aline {
				t.Errorf("case %s line %d not equal:\n  expected: %s\n  received: %s", file, i, eline, aline)
				continue Files
			}
		}
		if len(expectedLines) < len(mock.ops) {
			t.Errorf("testee did unexpected tailing operation: %s", mock.ops[len(expectedLines)])
		}
	}
}

type mockBuilder struct {
	ops []string
	err error
}

func (s *mockBuilder) add(op string) {
	s.ops = append(s.ops, op)
}

func (s *mockBuilder) AddEnv(e map[string]string) error {
	s.add("ENV " + mapToString(e))
	return s.err
}

func (s *mockBuilder) AddExposedPorts(p []string) error {
	s.add("EXPOSE " + strings.Join(p, " "))
	return s.err
}

func (s *mockBuilder) AddLabels(l map[string]string) error {
	s.add("LABEL " + mapToString(l))
	return s.err
}

func (s *mockBuilder) AddVolumes(v []string) error {
	s.add("VOLUME " + sliceToString(v))
	return s.err
}

func (s *mockBuilder) CopyFile(contextDir string, srcPattern []string, dest string, user *idutils.User) error {
	u := "nil"
	if user != nil {
		u = user.String()
	}
	s.add("COPY " + strconv.Quote(contextDir) + " " + sliceToString(srcPattern) + " " + strconv.Quote(dest) + " " + u)
	return s.err
}

func (s *mockBuilder) FromImage(name string) error {
	s.add("FROM " + name)
	return s.err
}

func (s *mockBuilder) Run(args []string, addEnv map[string]string) error {
	s.add("RUN " + strings.TrimSpace(mapToString(addEnv)+" ") + sliceToString(args))
	return s.err
}

func (s *mockBuilder) SetAuthor(a string) error {
	s.add("AUTHOR " + strconv.Quote(a))
	return s.err
}

func (s *mockBuilder) SetCmd(c []string) error {
	s.add("CMD " + sliceToString(c))
	return s.err
}

func (s *mockBuilder) SetEntrypoint(e []string) error {
	s.add("ENTRYPOINT " + sliceToString(e))
	return s.err
}

func (s *mockBuilder) SetStopSignal(sig string) error {
	s.add("STOPSIGNAL " + sig)
	return s.err
}

func (s *mockBuilder) SetUser(u string) error {
	s.add("USER " + u)
	return s.err
}

func (s *mockBuilder) SetWorkingDir(w string) error {
	s.add("WORKDIR " + w)
	return s.err
}

func mapToString(m map[string]string) string {
	l := []string{}
	for k, v := range m {
		l = append(l, strconv.Quote(k)+"="+strconv.Quote(v))
	}
	sort.Strings(l)
	return strings.Join(l, " ")
}

func sliceToString(l []string) string {
	r := []string{}
	for _, e := range l {
		r = append(r, strconv.Quote(e))
	}
	return strings.Join(r, " ")
}
