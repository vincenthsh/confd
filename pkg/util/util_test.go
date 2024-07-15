package util

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/abtreece/confd/pkg/log"
	"github.com/spf13/afero"
)

// createDirStructure() creates the following directory structure:
//
//	├── other
//	│   ├── sym1.toml
//	│   └── sym2.toml
//	└── root
//	    ├── root.other1
//	    ├── root.toml
//	    ├── subDir1
//	    │   ├── sub1.other
//	    │   ├── sub1.toml
//	    │   └── sub12.toml
//	    ├── subDir2
//	    │   ├── sub2.other
//	    │   ├── sub2.toml
//	    │   ├── sub22.toml
//	    │   └── subSubDir
//	    │       ├── subsub.other
//	    │       ├── subsub.toml
//	    │       ├── subsub2.toml
//	    │       └── sym2.toml -> ../../../other/sym2.toml
//	    └── sym1.toml -> ../other/sym1.toml
func createDirStructure(fs afero.Fs) (string, error) {
	mod := os.FileMode(0755)
	flag := os.O_RDWR | os.O_CREATE | os.O_EXCL
	tmpDir, err := afero.TempDir(fs, "", "")
	if err != nil {
		return "", err
	}

	otherDir := filepath.Join(tmpDir, "other")
	err = fs.Mkdir(otherDir, mod)
	if err != nil {
		return "", err
	}
	_, err = fs.OpenFile(otherDir+"/sym1.toml", flag, mod)
	if err != nil {
		return "", err
	}
	_, err = fs.OpenFile(otherDir+"/sym2.toml", flag, mod)
	if err != nil {
		return "", err
	}

	rootDir := filepath.Join(tmpDir, "root")
	err = fs.Mkdir(rootDir, mod)
	if err != nil {
		return "", err
	}
	_, err = fs.OpenFile(rootDir+"/root.toml", flag, mod)
	if err != nil {
		return "", err
	}
	_, err = fs.OpenFile(rootDir+"/root.other1", flag, mod)
	if err != nil {
		return "", err
	}

	if linker, ok := fs.(afero.Linker); ok {
		err = linker.SymlinkIfPossible(otherDir+"/sym1.toml", rootDir+"/sym1.toml")
		if err != nil {
			return "", err
		}
		err = linker.SymlinkIfPossible(otherDir, rootDir+"/other")
		if err != nil {
			return "", err
		}
	}

	subDir := filepath.Join(rootDir, "subDir1")
	err = fs.Mkdir(subDir, mod)
	if err != nil {
		return "", err
	}
	_, err = fs.OpenFile(subDir+"/sub1.toml", flag, mod)
	if err != nil {
		return "", err
	}
	_, err = fs.OpenFile(subDir+"/sub12.toml", flag, mod)
	if err != nil {
		return "", err
	}
	_, err = fs.OpenFile(subDir+"/sub1.other", flag, mod)
	if err != nil {
		return "", err
	}
	subDir2 := filepath.Join(rootDir, "subDir2")
	err = fs.Mkdir(subDir2, mod)
	if err != nil {
		return "", err
	}
	_, err = fs.OpenFile(subDir2+"/sub2.toml", flag, mod)
	if err != nil {
		return "", err
	}
	_, err = fs.OpenFile(subDir2+"/sub22.toml", flag, mod)
	if err != nil {
		return "", err
	}
	_, err = fs.OpenFile(subDir2+"/sub2.other", flag, mod)
	if err != nil {
		return "", err
	}
	subSubDir := filepath.Join(subDir2, "subSubDir")
	err = fs.Mkdir(subSubDir, mod)
	if err != nil {
		return "", err
	}
	_, err = fs.OpenFile(subSubDir+"/subsub.toml", flag, mod)
	if err != nil {
		return "", err
	}
	_, err = fs.OpenFile(subSubDir+"/subsub2.toml", flag, mod)
	if err != nil {
		return "", err
	}
	_, err = fs.OpenFile(subSubDir+"/subsub.other", flag, mod)
	if err != nil {
		return "", err
	}
	if linker, ok := fs.(afero.Linker); ok {
		err = linker.SymlinkIfPossible(otherDir+"/sym2.toml", subSubDir+"/sym2.toml")
		if err != nil {
			return "", err
		}
	}

	// tmpDir may contain symlinks itself
	actualTmpDir, err := filepath.EvalSymlinks(tmpDir)
	if err != nil {
		return "", err
	}
	return actualTmpDir, nil
}

func TestRecursiveFilesLookup(t *testing.T) {
	fs := afero.NewOsFs() // OS filesystem for symlink support
	log.SetLevel("warn")
	// Setup temporary directories
	rootDir, err := createDirStructure(fs)
	if err != nil {
		t.Errorf("Failed to create temp dirs: %s", err.Error())
	}
	defer fs.RemoveAll(rootDir)
	files, err := RecursiveFilesLookup(rootDir+"/root", "*toml")
	if err != nil {
		t.Errorf("Failed to run recursiveFindFiles, got error: " + err.Error())
	}
	sort.Strings(files)
	expectedFiles := []string{
		rootDir + "/other/" + "sym1.toml",
		rootDir + "/other/" + "sym2.toml",
		rootDir + "/root/" + "root.toml",
		rootDir + "/root/subDir1/" + "sub1.toml",
		rootDir + "/root/subDir1/" + "sub12.toml",
		rootDir + "/root/subDir2/" + "sub2.toml",
		rootDir + "/root/subDir2/" + "sub22.toml",
		rootDir + "/root/subDir2/subSubDir/" + "subsub.toml",
		rootDir + "/root/subDir2/subSubDir/" + "subsub2.toml",
	}
	if len(files) != len(expectedFiles) {
		t.Fatalf("Did not find expected files:\nExpected:\n\t%s\nFound:\n\t%s\n",
			strings.Join(expectedFiles, "\n\t"),
			strings.Join(files, "\n\t"))
	}
	for i, f := range expectedFiles {
		if f != files[i] {
			t.Fatalf("Did not find file %s\n", f)
		}
	}
}

func TestIsConfigChangedTrue(t *testing.T) {
	log.SetLevel("warn")
	fs := afero.NewOsFs() // posix stats doesn't support memMapFs
	src, err := afero.TempFile(fs, "", "src")
	defer fs.Remove(src.Name())
	if err != nil {
		t.Errorf(err.Error())
	}
	_, err = src.WriteString("foo")
	if err != nil {
		t.Errorf(err.Error())
	}
	dest, err := afero.TempFile(fs, "", "dest")
	defer fs.Remove(dest.Name())
	if err != nil {
		t.Errorf(err.Error())
	}
	_, err = dest.WriteString("foo")
	if err != nil {
		t.Errorf(err.Error())
	}
	status, err := IsConfigChanged(fs, src.Name(), dest.Name())
	if err != nil {
		t.Errorf(err.Error())
	}
	if status == true {
		t.Errorf("Expected IsConfigChanged(src, dest) to be %v, got %v", true, status)
	}
}

func TestIsConfigChangedFalse(t *testing.T) {
	log.SetLevel("warn")
	fs := afero.NewOsFs() // posix stats doesn't support memMapFs
	src, err := afero.TempFile(fs, "", "src")
	defer fs.Remove(src.Name())
	if err != nil {
		t.Errorf(err.Error())
	}
	_, err = src.WriteString("src")
	if err != nil {
		t.Errorf(err.Error())
	}
	dest, err := afero.TempFile(fs, "", "dest")
	defer fs.Remove(dest.Name())
	if err != nil {
		t.Errorf(err.Error())
	}
	_, err = dest.WriteString("dest")
	if err != nil {
		t.Errorf(err.Error())
	}
	status, err := IsConfigChanged(fs, src.Name(), dest.Name())
	if err != nil {
		t.Errorf(err.Error())
	}
	if status == false {
		t.Errorf("Expected sameConfig(src, dest) to be %v, got %v", false, status)
	}
}
