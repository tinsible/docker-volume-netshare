package drivers

import (
	"bytes"
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/dickeyxxx/netrc"
	"github.com/docker/go-plugins-helpers/volume"
	"os"
	"path/filepath"
	"strings"
	"os/exec"
	"bufio"
)

const (
	UsernameOpt = "username"
	PasswordOpt = "password"
	DomainOpt   = "domain"
	SecurityOpt = "security"
	UidOpt = "uid"
	GidOpt = "gid"
	VersOpt = "vers"
)

type cifsDriver struct {
	volumeDriver
	creds *cifsCreds
	netrc *netrc.Netrc
}

type cifsCreds struct {
	user     string
	pass     string
	domain   string
	security string
}

func NewCIFSDriver(root, mountsFilename string, user, pass, domain, security, netrc string) cifsDriver {
	d := cifsDriver{
		volumeDriver: newVolumeDriver(root, mountsFilename),
		creds:        &cifsCreds{user: user, pass: pass, domain: domain, security: security},
		netrc:        parseNetRC(netrc),
	}

	r := d.volumeDriver.List(volume.Request{})
	for _, v := range r.Volumes {
		cnt := d.mountm.Count(v.Name)
		if cnt > 0 {
			hostdir := mountpoint(d.root, v.Name)
			source := d.fixSource(v.Name)
			if !checkMount(source, hostdir) {
				log.Infof("Volume %s: mount point %s is not mounted but reported to have %d connections, resetting connection count to 0", v.Name, hostdir, cnt)
				d.mountm.ResetCount(v.Name)
				d.saveMounts()
			}
		}
	}
	return d
}

func parseNetRC(path string) *netrc.Netrc {
	if n, err := netrc.Parse(filepath.Join(path, ".netrc")); err == nil {
		return n
	} else {
		log.Warnf("Error: %s", err.Error())
	}
	return nil
}

func (c cifsDriver) Mount(r volume.Request) volume.Response {
	c.m.Lock()
	defer c.m.Unlock()
	defer c.saveMounts()
	hostdir := mountpoint(c.root, r.Name)
	source := c.fixSource(r.Name)
	host := c.parseHost(r)

	log.Infof("Mount: %s, %v", r.Name, r.Options)

	if c.mountm.HasMount(r.Name) && c.mountm.Count(r.Name) > 0 {
		log.Infof("Using existing CIFS volume mount: %s", hostdir)
		c.mountm.Increment(r.Name)
		return volume.Response{Mountpoint: hostdir}
	}

	log.Infof("Mounting CIFS volume %s on %s", source, hostdir)

	if err := createDest(hostdir); err != nil {
		return volume.Response{Err: err.Error()}
	}

	if err := c.mountVolume(r.Name, source, hostdir, c.getCreds(host)); err != nil {
		return volume.Response{Err: err.Error()}
	}
	c.mountm.Add(r.Name, hostdir)
	return volume.Response{Mountpoint: hostdir}
}

func checkMount(source, hostdir string) bool {
	if out, err := exec.Command("sh", "-c", "mount -t cifs").CombinedOutput(); err != nil {
		log.Println(string(out))
		return false
	} else {
		scanner := bufio.NewScanner(strings.NewReader(string(out)))
		prefix := fmt.Sprintf("%s on %s", source, hostdir)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, prefix) {
				return true
			}
		}
		return false
	}
}

func (c cifsDriver) Unmount(r volume.Request) volume.Response {
	c.m.Lock()
	defer c.m.Unlock()
	defer c.saveMounts()
	hostdir := mountpoint(c.root, r.Name)
	source := c.fixSource(r.Name)

	if !c.mountm.HasMount(r.Name) {
		return volume.Response{}
	}

	if c.mountm.Count(r.Name) > 1 {
		log.Infof("Skipping unmount for %s - in use by other containers", r.Name)
		c.mountm.Decrement(r.Name)
		return volume.Response{}
	}

	if c.mountm.Count(r.Name) == 0 {
		log.Infof("Skipping unmount for %s - not mounted", r.Name)
		return volume.Response{}
	}

	c.mountm.Decrement(r.Name)

	log.Infof("Unmounting volume %s from %s", source, hostdir)

	if err := run(fmt.Sprintf("umount %s", hostdir)); err != nil {
		return volume.Response{Err: err.Error()}
	}

	c.mountm.DeleteIfNotManaged(r.Name)

	if err := os.RemoveAll(hostdir); err != nil {
		return volume.Response{Err: err.Error()}
	}

	return volume.Response{}
}

func (c cifsDriver) fixSource(name string) string {
	if c.mountm.HasOption(name, ShareOpt) {
		return "//" + c.mountm.GetOption(name, ShareOpt)
	}
	return "//" + name
}

func (c cifsDriver) parseHost(r volume.Request) string {
	name := r.Name
	if c.mountm.HasOption(r.Name, ShareOpt) {
		name = c.mountm.GetOption(r.Name, ShareOpt)
	}

	if strings.ContainsAny(name, "/") {
		s := strings.Split(name, "/")
		return s[0]
	}
	return name
}

func (s cifsDriver) mountVolume(name, source, dest string, creds *cifsCreds) error {
	var opts bytes.Buffer

	opts.WriteString("-o '")
	var user = creds.user
	var pass = creds.pass
	var domain = creds.domain
	var security = creds.security
	var uid, gid string
	var vers string

	if s.mountm.HasOptions(name) {
		mopts := s.mountm.GetOptions(name)
		if v, found := mopts[UsernameOpt]; found {
			user = v
		}
		if v, found := mopts[PasswordOpt]; found {
			pass = v
		}
		if v, found := mopts[DomainOpt]; found {
			domain = v
		}
		if v, found := mopts[SecurityOpt]; found {
			security = v
		}
		if v, found := mopts[UidOpt]; found {
			uid = v
		}
		if v, found := mopts[GidOpt]; found {
			gid = v
		}
		if v, found := mopts[VersOpt]; found {
			vers = v
		}
	}

	if user != "" {
		opts.WriteString(fmt.Sprintf("username=%s,", user))
		if pass != "" {
			opts.WriteString(fmt.Sprintf("password=%s,", pass))
		}
	} else {
		opts.WriteString("guest,")
	}

	if domain != "" {
		opts.WriteString(fmt.Sprintf("domain=%s,", domain))
	}

	if security != "" {
		opts.WriteString(fmt.Sprintf("sec=%s,", security))
	}

	if uid != "" {
		opts.WriteString(fmt.Sprintf("uid=%s,", uid))
	}

	if gid != "" {
		opts.WriteString(fmt.Sprintf("gid=%s,", gid))
	}

	if vers != "" {
		log.Debugf("Vers option present: %s", vers)
		opts.WriteString(fmt.Sprintf("vers=%s,", vers))
	}

	opts.WriteString("rw' ")

	opts.WriteString(fmt.Sprintf("%s %s", source, dest))
	cmd := fmt.Sprintf("mount -t cifs %s", opts.String())
	log.Debugf("Executing: %s\n", strings.Replace(cmd, "password="+pass, "password=****", 1))
	return run(cmd)
}

func (s cifsDriver) getCreds(host string) *cifsCreds {
	log.Debugf("GetCreds: host=%s, netrc=%v", host, s.netrc)
	if s.netrc != nil {
		m := s.netrc.Machine(host)
		if m != nil {
			return &cifsCreds{
				user:     m.Get("username"),
				pass:     m.Get("password"),
				domain:   m.Get("domain"),
				security: m.Get("security"),
			}
		}
	}
	return s.creds
}
