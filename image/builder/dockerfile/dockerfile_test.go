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
	"github.com/opencontainers/go-digest"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDockerfileApply(t *testing.T) {
	files, err := filepath.Glob("testfiles/*.test")
	require.NoError(t, err)
	applyStageTested := false
Files:
	for _, file := range files {
		fmt.Println("CASE ", file)
		efile, err := os.Open(file[0:len(file)-4] + "expected")
		require.NoError(t, err)
		defer efile.Close()
		b, err := ioutil.ReadAll(efile)
		require.NoError(t, err, file)
		expected := strings.TrimSpace(string(b))
		expectedLinesBr := strings.Split(expected, "\n")
		expectedLines := make([]string, 0, len(expectedLinesBr))
		for _, eline := range expectedLinesBr {
			if eline != "" {
				expectedLines = append(expectedLines, eline)
			}
		}
		testee := newTestee(t, file)
		require.NoError(t, err, file)
		mock := mockBuilder{returnErr: -1}
		err = testee.Apply(&mock)
		require.NoError(t, err)
		for i, eline := range expectedLines {
			aline := ""
			if len(mock.ops) > i {
				aline = mock.ops[i]
			}
			if eline != aline {
				t.Errorf("%s: line %d not equal:\n  expected: %s\n  received: %s", filepath.Base(file), i, eline, aline)
				continue Files
			}
		}
		if len(expectedLines) < len(mock.ops) {
			t.Errorf("%s: testee did unexpected tailing operation: %s", filepath.Base(file), mock.ops[len(expectedLines)])
		}

		// Test error handling
		returnErr := 0
		lastOpCount := 0
		for {
			testee = newTestee(t, file)
			mock = mockBuilder{returnErr: returnErr}
			err = testee.Apply(&mock)
			if mock.returnCount == lastOpCount {
				break
			}
			if mock.returnCount != lastOpCount+1 {
				t.Errorf("%s: builder error not handled in %q", filepath.Base(file), mock.ops[len(mock.ops)-1])
				break
			}
			if err == nil {
				t.Errorf("%s: builder error not returned in %q", filepath.Base(file), mock.ops[len(mock.ops)-1])
				break
			}
			lastOpCount = mock.returnCount
			returnErr += 1
		}
		if lastOpCount < 2 {
			t.Errorf("%s: test failed too early on builder error (or case contains <2 instructions)", file)
		}

		// Test single stage execution
		if strings.Contains(file, "multistage") {
			applyStageTested = true
			expectedOps := mock.ops[mock.stage2OpOffset:mock.stage6OpOffset]
			//panic(fmt.Sprintf("%d %s", mock.stage2OpOffset, strings.Join(expectedOps, "\n")))
			testee := newTestee(t, file)
			require.NoError(t, err, file)
			mock = mockBuilder{returnErr: -1, stageCount: 1}
			err = testee.Target("slim")
			require.NoError(t, err, file)
			err = testee.Apply(&mock)
			require.NoError(t, err, file)
			if !assert.Equal(t, expectedOps, mock.ops, filepath.Base(file)+": apply slim stage") {
				t.FailNow()
			}
		}
	}
	if !applyStageTested {
		t.Errorf("ApplyStage() has not been tested")
	}
}

func newTestee(t *testing.T, file string) *DockerfileBuilder {
	args := map[string]string{
		"argp": "pval",
	}
	contents, err := ioutil.ReadFile(file)
	require.NoError(t, err)
	r, err := LoadDockerfile(contents, "./ctx", args, log.New(os.Stderr, "warn: "+file+":", 0))
	require.NoError(t, err)
	return r
}

type mockBuilder struct {
	ops            []string
	returnErr      int
	returnCount    int
	stageCount     int
	stage2OpOffset int
	stage6OpOffset int
}

func (s *mockBuilder) err() (err error) {
	if s.returnCount == s.returnErr {
		err = errors.New("expected error")
	}
	s.returnCount++
	return
}

func (s *mockBuilder) add(op string) {
	s.ops = append(s.ops, op)
}

func (s *mockBuilder) Image() digest.Digest {
	return digest.Digest("stage" + strconv.Itoa(s.stageCount-1) + "-image")
}

func (s *mockBuilder) AddEnv(e map[string]string) error {
	s.add("ENV " + mapToString(e))
	return s.err()
}

func (s *mockBuilder) AddExposedPorts(p []string) error {
	s.add("EXPOSE " + strings.Join(p, " "))
	return s.err()
}

func (s *mockBuilder) AddLabels(l map[string]string) error {
	s.add("LABEL " + mapToString(l))
	return s.err()
}

func (s *mockBuilder) AddVolumes(v []string) error {
	s.add("VOLUME " + sliceToString(v))
	return s.err()
}

func (s *mockBuilder) AddFiles(srcDir string, srcPattern []string, dest string, user *idutils.User) error {
	u := "nil"
	if user != nil {
		u = user.String()
	}
	s.add(fmt.Sprintf("ADD dir=%q %s %q %s", srcDir, sliceToString(srcPattern), dest, u))
	return s.err()
}

func (s *mockBuilder) CopyFiles(srcDir string, srcPattern []string, dest string, user *idutils.User) error {
	u := "nil"
	if user != nil {
		u = user.String()
	}
	s.add(fmt.Sprintf("COPY dir=%q %s %q %s", srcDir, sliceToString(srcPattern), dest, u))
	return s.err()
}

func (s *mockBuilder) CopyFilesFromImage(srcImage string, srcPattern []string, dest string, user *idutils.User) error {
	u := "nil"
	if user != nil {
		u = user.String()
	}
	s.add(fmt.Sprintf("COPY image=%q %s %q %s", srcImage, sliceToString(srcPattern), dest, u))
	return s.err()
}

func (s *mockBuilder) FromImage(name string) error {
	s.add("FROM " + name)
	s.stageCount++
	if s.stageCount == 2 {
		s.stage2OpOffset = len(s.ops) - 1
	}
	if s.stageCount == 6 {
		s.stage6OpOffset = len(s.ops) - 1
	}
	return s.err()
}

func (s *mockBuilder) Run(args []string, addEnv map[string]string) error {
	s.add("RUN " + strings.TrimSpace(mapToString(addEnv)+" "+sliceToString(args)))
	return s.err()
}

func (s *mockBuilder) SetAuthor(a string) error {
	s.add("AUTHOR " + strconv.Quote(a))
	return s.err()
}

func (s *mockBuilder) SetCmd(c []string) error {
	s.add("CMD " + sliceToString(c))
	return s.err()
}

func (s *mockBuilder) SetEntrypoint(e []string) error {
	s.add("ENTRYPOINT " + sliceToString(e))
	return s.err()
}

func (s *mockBuilder) SetStopSignal(sig string) error {
	s.add("STOPSIGNAL " + sig)
	return s.err()
}

func (s *mockBuilder) SetUser(u string) error {
	s.add("USER " + u)
	return s.err()
}

func (s *mockBuilder) SetWorkingDir(w string) error {
	s.add("WORKDIR " + w)
	return s.err()
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
