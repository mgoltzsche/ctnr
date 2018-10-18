package source

import (
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/mgoltzsche/ctnr/pkg/fs"
	"github.com/mgoltzsche/ctnr/pkg/idutils"
	"github.com/pkg/errors"
)

var _ fs.Source = &sourceURL{}

type sourceURL struct {
	fs.FileAttrs
	fs.DerivedAttrs
	cache HttpHeaderCache
}

func NewSourceURL(url *url.URL, etagCache HttpHeaderCache, chown idutils.UserIds) fs.Source {
	return &sourceURL{fs.FileAttrs{UserIds: chown, Mode: 0600}, fs.DerivedAttrs{URL: url.String()}, etagCache}
}

func (s *sourceURL) Attrs() fs.NodeInfo {
	return fs.NodeInfo{fs.TypeFile, s.FileAttrs}
}

// Performs an HTTP cache validation request and/or obtains new header values.
// Either the cached or the new values are returned and can be used as cache key.
func (s *sourceURL) DeriveAttrs() (a fs.DerivedAttrs, err error) {
	if s.HTTPInfo == "" {
		defer func() {
			if err != nil {
				err = errors.Wrapf(err, "source URL %s", s.URL)
			}
		}()
		var (
			cachedAttrs *HttpHeaders
			req         *http.Request
			res         *http.Response
			client      = &http.Client{}
		)
		if cachedAttrs, err = s.cache.GetHttpHeaders(s.URL); err != nil {
			return
		}
		if req, err = http.NewRequest(http.MethodGet, s.URL, nil); err != nil {
			return
		}
		if cachedAttrs != nil {
			if cachedAttrs.Etag != "" {
				req.Header.Set("If-None-Match", cachedAttrs.Etag)
			}
			if cachedAttrs.LastModified != "" {
				req.Header.Set("If-Modified-Since", cachedAttrs.LastModified)
			}
		}
		if res, err = client.Do(req); err != nil {
			return
		}
		defer func() {
			if e := res.Body.Close(); e != nil && err == nil {
				err = e
			}
		}()
		if res.StatusCode == 304 && cachedAttrs != nil {
			s.setUrlInfo(cachedAttrs)
		} else if res.StatusCode == 200 {
			urlInfo := urlInfo(res)
			if err = s.cache.PutHttpHeaders(s.URL, urlInfo); err != nil {
				return
			}
			s.setUrlInfo(urlInfo)
		} else {
			return a, errors.Errorf("returned HTTP code %d %s", res.StatusCode, res.Status)
		}
	}
	return s.DerivedAttrs, nil
}

func urlInfo(r *http.Response) *HttpHeaders {
	return &HttpHeaders{
		r.ContentLength,
		r.Header.Get("ETag"),
		r.Header.Get("Last-Modified"),
	}
}

func (s *sourceURL) setUrlInfo(a *HttpHeaders) {
	info := ""
	if a.Etag != "" {
		info = "etag:" + url.QueryEscape(a.Etag)
	}
	if a.LastModified != "" {
		if info != "" {
			info += ","
		}
		info += "time:" + url.QueryEscape(a.LastModified)
	} else if a.ContentLength > 0 && a.Etag == "" {
		info = "size=" + strconv.FormatInt(a.ContentLength, 10)
	}
	s.HTTPInfo = info
	s.Size = a.ContentLength
}

func (s *sourceURL) Write(dest, name string, w fs.Writer, _ map[fs.Source]string) (err error) {
	_, err = w.File(dest, s)
	return errors.Wrap(err, "source URL")
}

func (s *sourceURL) Reader() (io.ReadCloser, error) {
	res, err := http.Get(s.URL)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != 200 {
		res.Body.Close()
		return nil, errors.Errorf("source URL %s: returned status %d %s", s.URL, res.StatusCode, res.Status)
	}
	// Size must be set here in order to stream URL into tar
	s.Size = res.ContentLength
	return res.Body, nil
}

func (s *sourceURL) HashIfAvailable() string {
	return ""
}

func (s *sourceURL) Equal(o fs.Source) (bool, error) {
	if s.Attrs().Equal(o.Attrs()) {
		return false, nil
	}
	oa, err := o.DeriveAttrs()
	if err != nil {
		return false, errors.Wrap(err, "equal")
	}
	a, err := s.DeriveAttrs()
	return a.Equal(&oa), errors.Wrap(err, "equal")
}

func (s *sourceURL) String() string {
	return "sourceURL{" + s.URL + "}"
}
