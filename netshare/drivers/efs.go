package drivers

import (
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/docker/go-plugins-helpers/volume"
	"os"
	"strings"
)

const (
	EfsTemplateURI = "%s.%s.efs.%s.amazonaws.com"
)

type efsDriver struct {
	volumeDriver
	availzone string
	resolve   bool
	region    string
	resolver  *Resolver
	dnscache  map[string]string
}

func NewEFSDriver(root, mountsFilename string, az, nameserver string, resolve bool) efsDriver {

	d := efsDriver{
		volumeDriver: newVolumeDriver(root, mountsFilename),
		resolve:      resolve,
		dnscache:     map[string]string{},
	}

	if resolve {
		d.resolver = NewResolver(nameserver)
	}
	md, err := fetchAWSMetaData()
	if err != nil {
		log.Fatalf("Error resolving AWS metadata: %s", err.Error())
		os.Exit(1)
	}
	d.region = md.Region
	if az == "" {
		d.availzone = md.AvailZone
	}
	return d
}

func (e efsDriver) Mount(r volume.Request) volume.Response {
	e.m.Lock()
	defer e.m.Unlock()
	defer e.saveMounts()
	hostdir := mountpoint(e.root, r.Name)
	source := e.fixSource(r)

	if e.mountm.HasMount(r.Name) && e.mountm.Count(r.Name) > 0 {
		log.Infof("Using existing EFS volume mount: %s", hostdir)
		e.mountm.Increment(r.Name)
		return volume.Response{Mountpoint: hostdir}
	}

	log.Infof("Mounting EFS volume %s on %s", source, hostdir)

	if err := createDest(hostdir); err != nil {
		return volume.Response{Err: err.Error()}
	}

	if err := e.mountVolume(source, hostdir); err != nil {
		return volume.Response{Err: err.Error()}
	}
	e.mountm.Add(r.Name, hostdir)
	return volume.Response{Mountpoint: hostdir}
}

func (e efsDriver) Unmount(r volume.Request) volume.Response {
	e.m.Lock()
	defer e.m.Unlock()
	defer e.saveMounts()
	hostdir := mountpoint(e.root, r.Name)
	source := e.fixSource(r)

	if e.mountm.HasMount(r.Name) {
		if e.mountm.Count(r.Name) > 1 {
			log.Infof("Skipping unmount for %s - in use by other containers", hostdir)
			e.mountm.Decrement(r.Name)
			return volume.Response{}
		}
		e.mountm.Decrement(r.Name)
	}

	log.Infof("Unmounting volume %s from %s", source, hostdir)

	if err := run(fmt.Sprintf("umount %s", hostdir)); err != nil {
		return volume.Response{Err: err.Error()}
	}

	e.mountm.DeleteIfNotManaged(r.Name)

	if err := os.RemoveAll(r.Name); err != nil {
		return volume.Response{Err: err.Error()}
	}

	return volume.Response{}
}

func (e efsDriver) fixSource(r volume.Request) string {
	name := r.Name
	if e.mountm.HasOption(r.Name, ShareOpt) {
		name = e.mountm.GetOption(r.Name, ShareOpt)
	}

	v := strings.Split(name, "/")
	if e.resolve {
		uri := fmt.Sprintf(EfsTemplateURI, e.availzone, v[0], e.region)
		if i, ok := e.dnscache[uri]; ok {
			return mountSuffix(i)
		}

		log.Debugf("Attempting to resolve: %s", uri)
		if ip, err := e.resolver.Lookup(uri); err == nil {
			log.Debugf("Resolved Addresses: %s", ip)
			e.dnscache[uri] = ip
			return mountSuffix(ip)
		} else {
			log.Errorf("Error during resolve: %s", err.Error())
			return mountSuffix(uri)
		}
	}
	v[0] = v[0] + ":"
	return strings.Join(v, "/")
}

func mountSuffix(uri string) string {
	return uri + ":/"
}

func (e efsDriver) mountVolume(source, dest string) error {
	cmd := fmt.Sprintf("mount -t nfs4 %s %s", source, dest)
	log.Debugf("exec: %s\n", cmd)
	return run(cmd)
}
