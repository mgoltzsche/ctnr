package compose

import (
	"fmt"
	"io/ioutil"
	"testing"

	"github.com/mgoltzsche/cntnr/model"
	"github.com/mgoltzsche/cntnr/pkg/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad(t *testing.T) {
	b, err := ioutil.ReadFile("full-example.json")

	/*if err = os.Chdir("../../vendor/github.com/docker/cli/cli/compose/loader"); err != nil {
		t.Fatal(err)
		t.FailNow()
	}*/

	require.NoError(t, err)
	expected, err := model.FromJSON(b)
	require.NoError(t, err)
	env := map[string]string{}
	env["HOME"] = "/home/user"
	actual, err := Load("../../vendor/github.com/docker/cli/cli/compose/loader/full-example.yml", "../../vendor/github.com/docker/cli/cli/compose/loader", env, log.NewNopLogger())
	require.NoError(t, err)
	fmt.Println(actual.JSON())
	assert.Equal(t, expected.Services, actual.Services)
	assert.Equal(t, expected.Volumes, actual.Volumes)
}
