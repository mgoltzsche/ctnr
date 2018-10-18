package source

import (
	"encoding/base64"
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/mgoltzsche/ctnr/pkg/atomic"
	"github.com/pkg/errors"
)

type HttpHeaderCache interface {
	// Restores cached attrs or null
	GetHttpHeaders(url string) (*HttpHeaders, error)
	// Stores URL attrs
	PutHttpHeaders(url string, attrs *HttpHeaders) error
}

type HttpHeaders struct {
	ContentLength int64
	Etag          string
	LastModified  string
}

type httpHeaderCache string

func NewHttpHeaderCache(dir string) HttpHeaderCache {
	return httpHeaderCache(dir)
}

func (c httpHeaderCache) GetHttpHeaders(url string) (*HttpHeaders, error) {
	b, err := ioutil.ReadFile(c.file(url))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "HTTPHeaderCache")
	}
	h := HttpHeaders{}
	return &h, errors.Wrap(json.Unmarshal(b, &h), "HTTPHeaderCache")
}

func (c httpHeaderCache) PutHttpHeaders(url string, attrs *HttpHeaders) error {
	file := c.file(url)
	os.MkdirAll(filepath.Dir(file), 0775)
	_, err := atomic.WriteJson(file, attrs)
	return errors.Wrap(err, "HTTPHeaderCache")
}

func (c httpHeaderCache) file(url string) string {
	return filepath.Join(string(c), base64.RawStdEncoding.EncodeToString([]byte(url)))
}

type NoopHttpHeaderCache string

func (c NoopHttpHeaderCache) GetHttpHeaders(url string) (*HttpHeaders, error) {
	return nil, nil
}

func (c NoopHttpHeaderCache) PutHttpHeaders(url string, attrs *HttpHeaders) error {
	return nil
}
