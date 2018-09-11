package builder

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/containers/image/types"
	bstore "github.com/mgoltzsche/cntnr/bundle/store"
	"github.com/mgoltzsche/cntnr/image"
	"github.com/mgoltzsche/cntnr/image/builder/dockerfile"
	istore "github.com/mgoltzsche/cntnr/image/store"
	extlog "github.com/mgoltzsche/cntnr/pkg/log"
	"github.com/mgoltzsche/cntnr/pkg/log/logrusadapt"
	"github.com/mgoltzsche/cntnr/store"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xeipuuv/gojsonpointer"
)

// Integration test
func TestImageBuilder(t *testing.T) {
	files, err := filepath.Glob("dockerfile/testfiles/*.test")
	require.NoError(t, err)
	tmpDir, err := ioutil.TempDir("", ".imagebuildertestdata-")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)
	srcDir := filepath.Join(tmpDir, "src")
	err = os.Mkdir(srcDir, 0755)
	require.NoError(t, err)
	for _, f := range []string{"entrypoint.sh", "cfg-a.conf", "cfg-b.conf"} {
		err = ioutil.WriteFile(filepath.Join(srcDir, f), []byte("x"), 0740)
		require.NoError(t, err)
	}
	wd, err := os.Getwd()
	require.NoError(t, err)
	err = os.Chdir(tmpDir)
	require.NoError(t, err)
	defer os.Chdir(wd)
	logger := logrus.New()
	logger.Level = logrus.DebugLevel
	logger.Out = os.Stdout
	loggers := extlog.Loggers{
		Error: logrusadapt.NewErrorLogger(logger),
		Warn:  logrusadapt.NewWarnLogger(logger),
		Info:  logrusadapt.NewInfoLogger(logger),
		Debug: logrusadapt.NewDebugLogger(logger),
	}

	var baseImg *image.Image

	for _, file := range files {
		if file == "dockerfile/testfiles/10-add.test" {
			continue
		}
		loggers.Info.Println("TEST CASE", file)
		withNewTestee(t, tmpDir, loggers, func(testee *ImageBuilder) {
			// Read input & assertion from file
			b, err := ioutil.ReadFile(filepath.Join(wd, file))
			require.NoError(t, err, filepath.Base(file))

			// Run test
			args := map[string]string{
				"argp": "pval",
			}
			testee.SetImageResolver(ResolveDockerImage)
			df, err := dockerfile.LoadDockerfile(b, srcDir, args, loggers.Warn)
			require.NoError(t, err, filepath.Base(file))
			err = df.Apply(testee)
			require.NoError(t, err, filepath.Base(file))
			imageId := testee.Image()
			assert.NotNil(t, imageId, "resulting image", filepath.Base(file))
			err = imageId.Validate()
			require.NoError(t, err, "resulting image ID", filepath.Base(file))
			if baseImg == nil {
				img, err := testee.images.ImageByName("docker://alpine:3.7")
				require.NoError(t, err, "get common base image from store after build completed")
				baseImg = &img
			}
			img, err := testee.images.Image(*imageId)
			require.NoError(t, err, filepath.Base(file)+" load resulting image")
			cfg, err := img.Config()
			require.NoError(t, err, filepath.Base(file)+" load resulting image config")

			// Assert
			assertions := []string{}
			for _, line := range strings.Split(string(b), "\n") {
				if strings.HasPrefix(line, "# ASSERT ") {
					assertions = append(assertions, line[9:])
				}
			}
			if len(assertions) == 0 {
				t.Errorf("No assertion found in %s", filepath.Base(file))
				t.FailNow()
			}

			for _, assertionExpr := range assertions {
				loggers.Info.Println("ASSERTION "+file+":", assertionExpr)
				switch assertionExpr[:3] {
				case "RUN":
					// Assert by running command
					cmd := assertionExpr[4:]
					err = testee.Run([]string{"/bin/sh", "-c", cmd}, nil)
					require.NoError(t, err, filepath.Base(file)+" assertion")
				case "CFG":
					// Assert by JSON query
					query := assertionExpr[4:]
					spacePos := strings.Index(query, "=")
					expected := query[spacePos+1:]
					query = query[:spacePos]
					assertPathEqual(t, &cfg, query, expected, filepath.Base(file)+" assertion query: "+query)
				default:
					t.Errorf("Unsupported assertion in %s: %q", filepath.Base(file), assertionExpr)
					t.FailNow()
				}
			}

			// Test image size: image is too big it is likely that fsspec integration doesn't work
			if img.Size() >= baseImg.Size()*2 {
				t.Errorf("the whole base image seems to be copied into the next layer because new image size >= base image size * 2")
				t.FailNow()
			}
		})
	}
}

func assertPathEqual(t *testing.T, o interface{}, query, expected, msg string) {
	jp, err := gojsonpointer.NewJsonPointer(query)
	require.NoError(t, err, msg)
	jsonDoc := map[string]interface{}{}
	b, err := json.Marshal(&o)
	require.NoError(t, err, msg)
	err = json.Unmarshal(b, &jsonDoc)
	require.NoError(t, err, msg)
	valueStr := ""
	match, _, err := jp.Get(jsonDoc)
	if expected != "" {
		require.NoError(t, err, msg)
	}
	if match != nil {
		valueStr = fmt.Sprintf("%s", match)
	}
	if !assert.Equal(t, expected, valueStr, msg) {
		t.FailNow()
	}
}

func withNewTestee(t *testing.T, tmpDir string, loggers extlog.Loggers, assertions func(*ImageBuilder)) {
	ctx := &types.SystemContext{DockerInsecureSkipTLSVerify: true}

	// Init image store
	storero, err := store.NewStore(filepath.Join(tmpDir, "image-store"), true, ctx, istore.TrustPolicyInsecure(), loggers)
	require.NoError(t, err)
	lockedStore, err := storero.OpenLockedImageStore()
	require.NoError(t, err)
	defer func() {
		if err := lockedStore.Close(); err != nil {
			t.Error("failed to close locked store: ", err)
		}
	}()

	// Init bundle store
	bundleStore, err := bstore.NewBundleStore(filepath.Join(tmpDir, "bundle-store"), loggers.Info, loggers.Debug)
	require.NoError(t, err)

	// Init testee
	testee := NewImageBuilder(ImageBuildConfig{
		Images:   lockedStore,
		Bundles:  bundleStore,
		Cache:    NewImageBuildCache(filepath.Join(tmpDir, "cache"), loggers.Warn),
		Tempfs:   filepath.Join(tmpDir, "tmp"),
		RunRoot:  filepath.Join(tmpDir, "run"),
		Rootless: true,
		PRoot:    "", // TODO: also test using proot
		Loggers:  loggers,
	})
	defer func() {
		if err := testee.Close(); err != nil {
			t.Error("failed to close image builder: ", err)
		}
	}()

	// Do tests
	assertions(testee)

	// TODO: test that tmp and run directories are empty after test finished
}
