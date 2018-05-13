package files

import (
	"testing"
)

func TestFileMode(t *testing.T) {
	for _, c := range []struct {
		t SourceType
		e string
	}{
		{TypeFile, "file"},
		{TypeSymlink, "link"},
		{TypeDir, "dir"},
		{TypeOverlay, "overlay"},
	} {
		if c.t.String() == "" {
			t.Errorf("type.String() empty")
		}
		if c.t.IsDir() {
			if c.e != "dir" && c.e != "overlay" {
				t.Errorf("%s.IsDir() should not be true", c.e)
			}
		} else if c.e == "dir" /* || c.e == "overlay"*/ {
			t.Errorf("%s.IsDir() should be true", c.e)
		}

		if c.t.IsSymlink() {
			if c.e != "link" {
				t.Errorf("%s.IsLink() should not be true", c.e)
			}
		} else if c.e == "link" {
			t.Errorf("%s.IsLink() should be true", c.e)
		}

		if c.t.IsOverlay() {
			if c.e != "overlay" {
				t.Errorf("%s.IsOverlay() should not be true", c.e)
			}
		} else if c.e == "overlay" {
			t.Errorf("%s.IsOverlay() should be true", c.e)
		}

		if c.t.IsFile() {
			if c.e != "file" && c.e != "link" {
				t.Errorf("%s.IsFile() should not be true", c.e)
			}
		} else if c.e == "file" {
			t.Errorf("%s.IsFile() should be true", c.e)
		}
	}
}
