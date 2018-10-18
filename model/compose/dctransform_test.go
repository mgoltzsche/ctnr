package compose

import (
	"fmt"
	"io/ioutil"
	"testing"

	"github.com/mgoltzsche/ctnr/model"
	"github.com/mgoltzsche/ctnr/pkg/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad(t *testing.T) {
	b, err := ioutil.ReadFile("full-example.json")
	require.NoError(t, err)
	expected, err := model.FromJSON(b)
	require.NoError(t, err)
	env := map[string]string{}
	env["HOME"] = "/home/user"
	actual, err := Load("../../vendor/github.com/docker/cli/cli/compose/loader/full-example.yml", "../../vendor/github.com/docker/cli/cli/compose/loader", env, log.NewNopLogger())
	require.NoError(t, err)
	if !assert.Equal(t, expected.Services, actual.Services) ||
		!assert.Equal(t, expected.Volumes, actual.Volumes) {
		fmt.Println(actual.JSON())
		t.FailNow()
	}
}
