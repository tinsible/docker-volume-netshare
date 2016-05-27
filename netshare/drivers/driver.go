package drivers

import (
	log "github.com/Sirupsen/logrus"
	"github.com/docker/go-plugins-helpers/volume"
	"sync"
	"io/ioutil"
	"encoding/json"
	"os"
)

type volumeDriver struct {
	root   string
	mountm *mountManager
	m      *sync.Mutex
	fname  string
}

func newVolumeDriver(root string, mountsFilename string) volumeDriver {
	mountm := NewVolumeManager()

	dat, err := ioutil.ReadFile(mountsFilename)
	if err == nil {
		err = json.Unmarshal(dat, mountm)
	}
	if err != nil {
		log.Warnf("Cannot read mounts file %s, will try to create a new one, error=%v", mountsFilename, err)
	}

	v := volumeDriver{
		root:   root,
		mountm: mountm,
		m:      &sync.Mutex{},
		fname:  mountsFilename,
	}
	v.saveMounts()

	return v
}

func (v volumeDriver) saveMounts() {
	b, err := json.Marshal(v.mountm)
	if err != nil {
		log.Errorf("Error marshaling mounts data to json, error=%v", err)
		panic(err)
	}

	err = ioutil.WriteFile(v.fname, b, 0600)
	if err != nil {
		log.Errorf("Error writing mounts file %s, error=%v, make sure that file's parent directory exists and is writable", v.fname, err)
		os.Exit(1)
	}
}

func (v volumeDriver) Create(r volume.Request) volume.Response {
	log.Debugf("Entering Create: name: %s, options %v", r.Name, r.Options)

	v.m.Lock()
	defer v.m.Unlock()
	defer v.saveMounts()

	log.Debugf("Create volume -> name: %s, %v", r.Name, r.Options)

	dest := mountpoint(v.root, r.Name)
	if err := createDest(dest); err != nil {
		return volume.Response{Err: err.Error()}
	}
	v.mountm.Create(r.Name, dest, r.Options)
	return volume.Response{}
}

func (v volumeDriver) Remove(r volume.Request) volume.Response {
	log.Debugf("Entering Remove: name: %s, options %v", r.Name, r.Options)
	v.m.Lock()
	defer v.m.Unlock()
	defer v.saveMounts()

	if err := v.mountm.Delete(r.Name); err != nil {
		return volume.Response{Err: err.Error()}
	}
	return volume.Response{}
}

func (v volumeDriver) Path(r volume.Request) volume.Response {
	log.Debugf("Host path for %s is at %s", r.Name, mountpoint(v.root, r.Name))
	return volume.Response{Mountpoint: mountpoint(v.root, r.Name)}
}

func (v volumeDriver) Get(r volume.Request) volume.Response {
	log.Debugf("Entering Get: %v", r)
	v.m.Lock()
	defer v.m.Unlock()
	hostdir := mountpoint(v.root, r.Name)

	if v.mountm.HasMount(r.Name) {
		log.Debugf("Get: mount found for %s, host directory: %s", r.Name, hostdir)
		return volume.Response{Volume: &volume.Volume{Name: r.Name, Mountpoint: hostdir}}
	}
	return volume.Response{}
}

func (v volumeDriver) List(r volume.Request) volume.Response {
	log.Debugf("Entering List: %v", r)
	return volume.Response{Volumes: v.mountm.GetVolumes(v.root)}
}
