# confd-testing

> [!WARNING]
> This is a (unmaintained) fork of [abtreece/confd](https://github.com/abtreece/confd) adding
> `afero` filesystem support for in-memory snapshot testing

Example code to snapshot test a confd configuration.

```golang
package test

import (
  "os"
  "path/filepath"
  "testing"

  "github.com/gkampitakis/go-snaps/snaps"
  "github.com/spf13/afero"

  "github.com/abtreece/confd/pkg/backends"
  "github.com/abtreece/confd/pkg/template"
  "github.com/stretchr/testify/require"
)

// fixed constants to prepare the afero filesystem for a test
const (
  confDir      = "confd"
  confFileName = "config.toml"
  tmplDir      = "templates"
  tmplFileName = "test.conf.tmpl"
  destDir      = "tmp"
  destFileName = "test.conf"
)

func TestConfdConfig(t *testing.T) {
  fs := afero.NewMemMapFs()
  
  // create directories in test fs
  err := fs.MkdirAll(confDir, os.ModePerm)
  require.NoError(t, err, "failed to create confd directory")
  err = fs.MkdirAll(tmplDir, os.ModePerm)
  require.NoError(t, err, "failed to create templates directory")
  err = fs.MkdirAll(destDir, os.ModePerm)
  require.NoError(t, err, "failed to create tmp directory")
  confPath := filepath.Join(confDir, confFileName)
  tmplPath := filepath.Join(tmplDir, tmplFileName)

  // copy testdata toml file to test fs
  toml, err := os.ReadFile("base-config.toml")
  require.NoError(t, err, "failed to read toml file")
  err = afero.WriteFile(fs, confPath, toml, os.ModePerm)

  // copy testdata tmpl file to test fs
  tmpl, err := os.ReadFile("base-test.conf.tmpl")
  require.NoError(t, err, "failed to read tmpl file")
  err = afero.WriteFile(fs, tmplPath, tmpl, os.ModePerm)
  require.NoError(t, err, "failed to write tmpl file")

  // create Template Resource
  backendConf := backends.Config{
    Backend: "env",
  }
  client, err := backends.New(backendConf)

  config := template.Config{
    StoreClient: client, // not used but must be set
    TemplateDir: tmplDir,
  }

  tr, err := template.NewTemplateResource(fs, confPath, config)
  if err != nil {
    return nil, err
  }
  // override config src and dest to verify contents
  tr.Src = tmplPath
  tr.Dest = filepath.Join(destDir, destFileName)
  tr.FileMode = 0666
  require.NoError(t, err)

  // prepare backend keys required for template under test
  tr.Store.Set("/test/key", "abc")

  // run template
  err = tr.CreateStageFile()
  require.NoError(t, err, "failed to create stage file")

  // snapshot template
  actual, err := afero.ReadFile(fs, tr.StageFile.Name())
  require.NoError(t, err, "failed to read StageFile")
  snaps.WithConfig(snaps.Filename("base")).MatchSnapshot(t, string(actual))
}
```
