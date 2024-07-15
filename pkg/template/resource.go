package template

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"text/template"

	"github.com/BurntSushi/toml"
	"github.com/abtreece/confd/pkg/backends"
	"github.com/abtreece/confd/pkg/log"
	util "github.com/abtreece/confd/pkg/util"
	"github.com/kelseyhightower/memkv"
	"github.com/spf13/afero"
)

type Config struct {
	ConfDir       string `toml:"confdir"`
	ConfigDir     string
	KeepStageFile bool
	Noop          bool   `toml:"noop"`
	Prefix        string `toml:"prefix"`
	StoreClient   backends.StoreClient
	SyncOnly      bool `toml:"sync-only"`
	TemplateDir   string
}

// TemplateResourceConfig holds the parsed template resource.
type TemplateResourceConfig struct {
	TemplateResource TemplateResource `toml:"template"`
}

// TemplateResource is the representation of a parsed template resource.
type TemplateResource struct {
	CheckCmd      string `toml:"check_cmd"`
	Dest          string
	FileMode      os.FileMode
	Gid           int
	Group         string
	Keys          []string
	Mode          string
	Owner         string
	Prefix        string
	ReloadCmd     string `toml:"reload_cmd"`
	Src           string
	StageFile     afero.File
	Uid           int
	funcMap       map[string]interface{}
	lastIndex     uint64
	keepStageFile bool
	noop          bool
	Store         memkv.Store
	storeClient   backends.StoreClient
	syncOnly      bool
	fs            afero.Fs
}

var ErrEmptySrc = errors.New("empty src template")

// NewTemplateResource creates a TemplateResource.
func NewTemplateResource(fs afero.Fs, path string, config Config) (*TemplateResource, error) {
	if config.StoreClient == nil {
		return nil, errors.New("A valid StoreClient is required.")
	}

	// Set the default uid and gid so we can determine if it was
	// unset from configuration.
	tc := &TemplateResourceConfig{TemplateResource{Uid: -1, Gid: -1}}

	log.Debug("Loading template resource from " + path)
	_, err := toml.DecodeFS(afero.NewIOFS(fs), path, &tc)
	if err != nil {
		return nil, fmt.Errorf("Cannot process template resource %s - %s", path, err.Error())
	}

	tr := tc.TemplateResource
	tr.keepStageFile = config.KeepStageFile
	tr.noop = config.Noop
	tr.storeClient = config.StoreClient
	tr.funcMap = newFuncMap()
	tr.Store = memkv.New()
	tr.syncOnly = config.SyncOnly
	tr.fs = fs
	addFuncs(tr.funcMap, tr.Store.FuncMap)

	if config.Prefix != "" {
		tr.Prefix = config.Prefix
	}

	if !strings.HasPrefix(tr.Prefix, "/") {
		tr.Prefix = "/" + tr.Prefix
	}

	if tr.Src == "" {
		return nil, ErrEmptySrc
	}

	if tr.Uid == -1 {
		if tr.Owner != "" {
			u, err := user.Lookup(tr.Owner)
			if err != nil {
				return nil, fmt.Errorf("Cannot find owner's UID - %s", err.Error())
			}
			tr.Uid, err = strconv.Atoi(u.Uid)
			if err != nil {
				return nil, fmt.Errorf("Cannot convert string to int - %s", err.Error())
			}
		} else {
			tr.Uid = os.Geteuid()
		}
	}

	if tr.Gid == -1 {
		if tr.Group != "" {
			g, err := user.LookupGroup(tr.Group)
			if err != nil {
				return nil, fmt.Errorf("Cannot find group's GID - %s", err.Error())
			}
			tr.Gid, err = strconv.Atoi(g.Gid)
			if err != nil {
				return nil, fmt.Errorf("Cannot convert string to int - %s", err.Error())
			}
		} else {
			tr.Gid = os.Getegid()
		}
	}

	tr.Src = filepath.Join(config.TemplateDir, tr.Src)
	return &tr, nil
}

// setVars sets the Vars for template resource.
func (t *TemplateResource) setVars() error {
	var err error
	log.Debug("Retrieving keys from store")
	log.Debug("Key prefix set to " + t.Prefix)

	result, err := t.storeClient.GetValues(util.AppendPrefix(t.Prefix, t.Keys))
	if err != nil {
		return err
	}
	log.Debug("Got the following map from store: %v", result)

	t.Store.Purge()

	for k, v := range result {
		t.Store.Set(path.Join("/", strings.TrimPrefix(k, t.Prefix)), v)
	}
	return nil
}

// CreateStageFile stages the src configuration file by processing the src
// template and setting the desired owner, group, and mode. It also sets the
// StageFile for the template resource.
// It returns an error if any.
func (t *TemplateResource) CreateStageFile() error {
	log.Debug("Using source template " + t.Src)

	if !util.IsFileExist(t.fs, t.Src) {
		return errors.New("Missing template: " + t.Src)
	}

	log.Debug("Compiling source template " + t.Src)

	tmpl, err := template.New(filepath.Base(t.Src)).Funcs(t.funcMap).ParseFS(afero.NewIOFS(t.fs), t.Src)
	if err != nil {
		return fmt.Errorf("Unable to process template %s, %s", t.Src, err)
	}

	// create TempFile in Dest directory to avoid cross-filesystem issues
	temp, err := afero.TempFile(t.fs, filepath.Dir(t.Dest), "."+filepath.Base(t.Dest))
	if err != nil {
		return err
	}

	if err = tmpl.Execute(temp, nil); err != nil {
		temp.Close()
		t.fs.Remove(temp.Name())
		return err
	}
	defer temp.Close()

	// Set the owner, group, and mode on the stage file now to make it easier to
	// compare against the destination configuration file later.
	t.fs.Chmod(temp.Name(), t.FileMode)
	t.fs.Chown(temp.Name(), t.Uid, t.Gid)
	t.StageFile = temp
	return nil
}

// sync compares the staged and dest config files and attempts to sync them
// if they differ. sync will run a config check command if set before
// overwriting the target config file. Finally, sync will run a reload command
// if set to have the application or service pick up the changes.
// It returns an error if any.
func (t *TemplateResource) sync() error {
	staged := t.StageFile.Name()
	if t.keepStageFile {
		log.Info("Keeping staged file: " + staged)
	} else {
		defer t.fs.Remove(staged)
	}

	log.Debug("Comparing candidate config to " + t.Dest)
	ok, err := util.IsConfigChanged(t.fs, staged, t.Dest)
	if err != nil {
		log.Error(err.Error())
	}
	if t.noop {
		log.Warning("Noop mode enabled. " + t.Dest + " will not be modified")
		return nil
	}
	if ok {
		log.Info("Target config " + t.Dest + " out of sync")
		if !t.syncOnly && t.CheckCmd != "" {
			if err := t.check(); err != nil {
				return errors.New("Config check failed: " + err.Error())
			}
		}
		log.Debug("Overwriting target config " + t.Dest)
		err := t.fs.Rename(staged, t.Dest)
		if err != nil {
			if strings.Contains(err.Error(), "device or resource busy") {
				log.Debug("Rename failed - target is likely a mount. Trying to write instead")
				// try to open the file and write to it
				var contents []byte
				var rerr error
				contents, rerr = afero.ReadFile(t.fs, staged)
				if rerr != nil {
					return rerr
				}
				err := afero.WriteFile(t.fs, t.Dest, contents, t.FileMode)
				// make sure owner and group match the temp file, in case the file was created with WriteFile
				t.fs.Chown(t.Dest, t.Uid, t.Gid)
				if err != nil {
					return err
				}
			} else {
				return err
			}
		}
		if !t.syncOnly && t.ReloadCmd != "" {
			if err := t.reload(); err != nil {
				return err
			}
		}
		log.Info("Target config " + t.Dest + " has been updated")
	} else {
		log.Debug("Target config " + t.Dest + " in sync")
	}
	return nil
}

// check executes the check command to validate the staged config file. The
// command is modified so that any references to src template are substituted
// with a string representing the full path of the staged file. This allows the
// check to be run on the staged file before overwriting the destination config
// file.
// It returns nil if the check command returns 0 and there are no other errors.
func (t *TemplateResource) check() error {
	var cmdBuffer bytes.Buffer
	data := make(map[string]string)
	data["src"] = t.StageFile.Name()
	tmpl, err := template.New("checkcmd").Parse(t.CheckCmd)
	if err != nil {
		return err
	}
	if err := tmpl.Execute(&cmdBuffer, data); err != nil {
		return err
	}
	return runCommand(cmdBuffer.String())
}

// reload executes the reload command.
// It returns nil if the reload command returns 0.
func (t *TemplateResource) reload() error {
	return runCommand(t.ReloadCmd)
}

// runCommand is a shared function used by check and reload
// to run the given command and log its output.
// It returns nil if the given cmd returns 0.
// The command can be run on unix and windows.
func runCommand(cmd string) error {
	log.Debug("Running " + cmd)
	var c *exec.Cmd
	if runtime.GOOS == "windows" {
		c = exec.Command("cmd", "/C", cmd)
	} else {
		c = exec.Command("/bin/sh", "-c", cmd)
	}

	output, err := c.CombinedOutput()
	if err != nil {
		log.Error(fmt.Sprintf("%q", string(output)))
		return err
	}
	log.Debug(fmt.Sprintf("%q", string(output)))
	return nil
}

// process is a convenience function that wraps calls to the three main tasks
// required to keep local configuration files in sync. First we gather vars
// from the store, then we stage a candidate configuration file, and finally sync
// things up.
// It returns an error if any.
func (t *TemplateResource) process() error {
	if err := t.setFileMode(); err != nil {
		return err
	}
	if err := t.setVars(); err != nil {
		return err
	}
	if err := t.CreateStageFile(); err != nil {
		return err
	}
	if err := t.sync(); err != nil {
		return err
	}
	return nil
}

// setFileMode sets the FileMode.
func (t *TemplateResource) setFileMode() error {
	if t.Mode == "" {
		if !util.IsFileExist(t.fs, t.Dest) {
			t.FileMode = 0644
		} else {
			fi, err := t.fs.Stat(t.Dest)
			if err != nil {
				return err
			}
			t.FileMode = fi.Mode()
		}
	} else {
		mode, err := strconv.ParseUint(t.Mode, 0, 32)
		if err != nil {
			return err
		}
		t.FileMode = os.FileMode(mode)
	}
	return nil
}
