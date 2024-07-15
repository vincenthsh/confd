package template

import (
	"os"
	"path/filepath"
	"testing"
	"text/template"

	"github.com/abtreece/confd/pkg/backends/env"
	"github.com/abtreece/confd/pkg/log"
	"github.com/spf13/afero"
)

// createTempDirs is a helper function which creates temporary directories
// required by confd. createTempDirs returns the path name representing the
// confd confDir.
// It returns an error if any.
func createTempDirs(fs afero.Fs) (string, error) {
	confDir, err := afero.TempDir(fs, "", "")
	if err != nil {
		return "", err
	}
	err = fs.Mkdir(filepath.Join(confDir, "templates"), 0755)
	if err != nil {
		return "", err
	}
	err = fs.Mkdir(filepath.Join(confDir, "conf.d"), 0755)
	if err != nil {
		return "", err
	}
	return confDir, nil
}

var templateResourceConfigTmpl = `
[template]
src = "{{.src}}"
dest = "{{.dest}}"
keys = [
  "foo",
]
`

func TestProcessTemplateResources(t *testing.T) {
	log.SetLevel("warn")
	fs := afero.NewOsFs() // Process uses os Fs
	// Setup temporary conf, config, and template directories.
	tempConfDir, err := createTempDirs(fs)
	if err != nil {
		t.Errorf("Failed to create temp dirs: %s", err.Error())
	}
	defer fs.RemoveAll(tempConfDir)

	// Create the src template.
	srcTemplateFile := filepath.Join(tempConfDir, "templates", "foo.tmpl")
	err = afero.WriteFile(fs, srcTemplateFile, []byte(`foo = {{getv "/foo"}}`), 0644)
	if err != nil {
		t.Error(err.Error())
	}

	// Create the dest.
	destFile, err := afero.TempFile(fs, "", "")
	if err != nil {
		t.Errorf("Failed to create destFile: %s", err.Error())
	}
	defer fs.Remove(destFile.Name())

	// Create the template resource configuration file.
	templateResourcePath := filepath.Join(tempConfDir, "conf.d", "foo.toml")
	templateResourceFile, err := fs.Create(templateResourcePath)
	if err != nil {
		t.Errorf(err.Error())
	}
	tmpl, err := template.New("templateResourceConfig").Parse(templateResourceConfigTmpl)
	if err != nil {
		t.Errorf("Unable to parse template resource template: %s", err.Error())
	}
	data := make(map[string]string)
	data["src"] = "foo.tmpl"
	data["dest"] = destFile.Name()
	err = tmpl.Execute(templateResourceFile, data)
	if err != nil {
		t.Errorf(err.Error())
	}

	os.Setenv("FOO", "bar")
	storeClient, err := env.NewEnvClient()
	if err != nil {
		t.Errorf(err.Error())
	}
	c := Config{
		ConfDir:     tempConfDir,
		ConfigDir:   filepath.Join(tempConfDir, "conf.d"),
		StoreClient: storeClient,
		TemplateDir: filepath.Join(tempConfDir, "templates"),
	}
	// Process the test template resource.
	err = Process(c)
	if err != nil {
		t.Error(err.Error())
	}
	// Verify the results.
	expected := "foo = bar"
	results, err := afero.ReadFile(fs, destFile.Name())
	if err != nil {
		t.Error(err.Error())
	}
	if string(results) != expected {
		t.Errorf("Expected contents of dest == '%s', got %s", expected, string(results))
	}
}
