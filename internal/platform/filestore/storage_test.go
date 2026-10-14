package filestore

import (
	"strings"
	"testing"
)

func TestFilenameTraversal(t *testing.T) {
	storage, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = storage.Save("../../outside.jpg", []byte("test")); err == nil {
		t.Fatal("path traversal accepted")
	}
	name := strings.Repeat("a", 64) + ".jpg"
	if err = storage.Save(name, []byte("image")); err != nil {
		t.Fatal(err)
	}
	if err = storage.Delete(name); err != nil {
		t.Fatal(err)
	}
}

func TestStoredFileSize(t *testing.T) {
	storage, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	name := strings.Repeat("b", 64) + ".jpg"
	if err = storage.Save(name, []byte("jpeg")); err != nil {
		t.Fatal(err)
	}
	size, err := storage.Size(name)
	if err != nil || size != 4 {
		t.Fatal("incorrect stored size", size, err)
	}
	if _, err = storage.Size("../secret"); err == nil {
		t.Fatal("size lookup allowed traversal")
	}
}
