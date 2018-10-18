package source

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/mgoltzsche/ctnr/pkg/fs"
	"github.com/mgoltzsche/ctnr/pkg/fs/testutils"
	"github.com/mgoltzsche/ctnr/pkg/idutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSourceURL(t *testing.T) {
	listener, url := mockHttpResource(t)
	defer listener.Close()
	writerMock := testutils.NewWriterMock(t, fs.AttrsAll)
	var mode os.FileMode = 0600
	usr := idutils.UserIds{1, 33}
	mockCache := mockedHttpCache{map[string]*HttpHeaders{}}
	testee := NewSourceURL(url, &mockCache, usr)

	a := testee.Attrs()
	if a.NodeType != fs.TypeFile {
		t.Errorf("Attrs(): type != TypeFile but %q", a.NodeType)
		t.FailNow()
	}
	if a.Mode != mode {
		t.Errorf("Attrs(): mode %s != %s", a.Mode, mode)
	}

	// Test derived attrs
	wa, err := testee.DeriveAttrs()
	require.NoError(t, err)
	if wa.URL != url.String() {
		t.Errorf("URL %q != %q", url, wa.URL)
	}
	if wa.Hash != "" {
		t.Errorf("hash != '' but %q", wa.Hash)
	}
	if wa.HTTPInfo == "" {
		t.Errorf("http cache info == ''")
	}
	etag1 := wa.HTTPInfo
	testee = NewSourceURL(url, &mockCache, usr)
	wa, err = testee.DeriveAttrs()
	require.NoError(t, err)
	etag2 := wa.HTTPInfo
	if etag1 != etag2 {
		t.Error("http info != http info")
	}

	// Test write
	testee.Write("/file", "", writerMock, nil)
	actual := strings.Join(writerMock.Written, "\n")
	expected := "/file type=file usr=1:33 mode=600 size=13 url=" + url.String() + " http=etag:mocked+%3D+etag1,time:Fri%2C+21+Sep+2018+20%3A52%3A35+GMT"
	assert.Equal(t, expected, actual)
}

func mockHttpResource(t *testing.T) (net.Listener, *url.URL) {
	reqCount := 0
	http.HandleFunc("/file", func(w http.ResponseWriter, r *http.Request) {
		reqCount++
		etag := r.Header.Get("If-None-Match")
		w.Header().Set("Last-Modified", "Fri, 21 Sep 2018 20:52:35 GMT")
		if etag == "" {
			w.Header().Set("ETag", fmt.Sprintf("mocked = etag%d", reqCount))
			w.WriteHeader(http.StatusOK)
		} else {
			w.Header().Set("ETag", etag)
			w.WriteHeader(http.StatusNotModified)
		}
		fmt.Fprintf(w, "servercontent")
	})
	listener, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	go http.Serve(listener, nil)
	url, err := url.Parse(fmt.Sprintf("http://127.0.0.1:%d/file", listener.Addr().(*net.TCPAddr).Port))
	require.NoError(t, err)
	return listener, url
}

type mockedHttpCache struct {
	entries map[string]*HttpHeaders
}

func (c *mockedHttpCache) GetHttpHeaders(url string) (*HttpHeaders, error) {
	return c.entries[url], nil
}
func (c *mockedHttpCache) PutHttpHeaders(url string, attrs *HttpHeaders) error {
	c.entries[url] = attrs
	return nil
}
