package drivers

import (
	"errors"
	log "github.com/Sirupsen/logrus"
	"github.com/docker/go-plugins-helpers/volume"
	"encoding/json"
)

const (
	ShareOpt = "share"
)

type mount struct {
	name        string
	hostdir     string
	connections int
	opts        map[string]string
	managed     bool
}

type mountManager struct {
	mounts map[string]*mount
}

func NewVolumeManager() *mountManager {
	return &mountManager{
		mounts: map[string]*mount{},
	}
}

func (m *mountManager) HasMount(name string) bool {
	_, found := m.mounts[name]
	return found
}

func (m *mountManager) HasOptions(name string) bool {
	c, found := m.mounts[name]
	if found {
		return c.opts != nil && len(c.opts) > 0
	}
	return false
}

func (m *mountManager) HasOption(name, key string) bool {
	if m.HasOptions(name) {
		if _, ok := m.mounts[name].opts[key]; ok {
			return ok
		}
	}
	return false
}

func (m *mountManager) GetOptions(name string) map[string]string {
	if m.HasOptions(name) {
		c, _ := m.mounts[name]
		return c.opts
	}
	return map[string]string{}
}

func (m *mountManager) GetOption(name, key string) string {
	if m.HasOption(name, key) {
		v, _ := m.mounts[name].opts[key]
		return v
	}
	return ""
}

func (m *mountManager) IsActiveMount(name string) bool {
	c, found := m.mounts[name]
	return found && c.connections > 0
}

func (m *mountManager) Count(name string) int {
	c, found := m.mounts[name]
	if found {
		return c.connections
	}
	return 0
}

func (m *mountManager) Add(name, hostdir string) {
	_, found := m.mounts[name]
	if found {
		m.Increment(name)
	} else {
		m.mounts[name] = &mount{name: name, hostdir: hostdir, managed: false, connections: 1}
	}
}

func (m *mountManager) Create(name, hostdir string, opts map[string]string) {
	c, found := m.mounts[name]
	if found && c.connections > 0 {
		c.opts = opts
	} else {
		m.mounts[name] = &mount{name: name, hostdir: hostdir, managed: true, opts: opts, connections: 0}
	}
}

func (m *mountManager) Delete(name string) error {
	if m.HasMount(name) {
		if m.Count(name) < 1 {
			delete(m.mounts, name)
			return nil
		}
		return errors.New("Volume is currently in use")
	}
	return nil
}

func (m *mountManager) DeleteIfNotManaged(name string) error {
	if m.HasMount(name) && !m.IsActiveMount(name) && !m.mounts[name].managed {
		log.Infof("Removing un-managed volume")
		return m.Delete(name)
	}
	return nil
}

func (m *mountManager) Increment(name string) int {
	c, found := m.mounts[name]
	if found {
		c.connections++
		return c.connections
	}
	return 0
}

func (m *mountManager) Decrement(name string) int {
	c, found := m.mounts[name]
	if found {
		c.connections--
	}
	return 0
}

func (m *mountManager) ResetCount(name string) int {
	c, found := m.mounts[name]
	if found {
		c.connections = 0
	}
	return 0
}

func (m *mountManager) GetVolumes(rootPath string) []*volume.Volume {

	volumes := []*volume.Volume{}

	for _, mount := range m.mounts {
		volumes = append(volumes, &volume.Volume{Name: mount.name, Mountpoint: mount.hostdir})
	}
	return volumes
}

type mountManagerDoc struct {
	Mounts map[string]*mount
}

func (m *mountManager) MarshalJSON() ([]byte, error) {
	return json.Marshal(mountManagerDoc{
		m.mounts,
	})
}

func (m *mountManager) UnmarshalJSON(b []byte) error {
	temp := &mountManagerDoc{}

	if err := json.Unmarshal(b, &temp); err != nil {
		return err
	}

	m.mounts = temp.Mounts

	return nil
}

type mountDoc struct {
	Name        string
	Hostdir     string
	Connections int
	Opts        map[string]string
	Managed     bool
}

func (m *mount) MarshalJSON() ([]byte, error) {
	return json.Marshal(mountDoc{
		m.name,
		m.hostdir,
		m.connections,
		m.opts,
		m.managed,
	})
}

func (m *mount) UnmarshalJSON(b []byte) error {
	temp := &mountDoc{}

	if err := json.Unmarshal(b, &temp); err != nil {
		return err
	}

	m.name = temp.Name
	m.hostdir = temp.Hostdir
	m.connections = temp.Connections
	m.opts = temp.Opts
	m.managed = temp.Managed

	return nil
}
