//
// Copyright 2019-2020 Nestybox, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

// +build linux

package syscont

import (
	"fmt"
	"path/filepath"
	"strings"

	mapset "github.com/deckarep/golang-set"
	ipcLib "github.com/nestybox/sysbox-ipc/sysboxMgrLib"
	utils "github.com/nestybox/sysbox-libs/utils"
	"github.com/opencontainers/runc/libsysbox/sysbox"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	"golang.org/x/sys/unix"
)

// Exported
const (
	IdRangeMin uint32 = 65536
)

// Internal
const (
	defaultUid uint32 = 231072
	defaultGid uint32 = 231072
)

var (
	SysboxFsDir string = "/var/lib/sysboxfs"
)

// System container "must-have" mounts
var sysboxMounts = []specs.Mount{
	specs.Mount{
		Destination: "/sys",
		Source:      "sysfs",
		Type:        "sysfs",
		Options:     []string{"noexec", "nosuid", "nodev"},
	},
	specs.Mount{
		Destination: "/sys/fs/cgroup",
		Source:      "cgroup",
		Type:        "cgroup",
		Options:     []string{"noexec", "nosuid", "nodev"},
	},
	// we don't yet virtualize configfs; create a dummy one.
	specs.Mount{
		Destination: "/sys/kernel/config",
		Source:      "tmpfs",
		Type:        "tmpfs",
		Options:     []string{"rw", "rprivate", "noexec", "nosuid", "nodev", "size=1m"},
	},
	// we don't virtualize debugfs; create a dummy one.
	specs.Mount{
		Destination: "/sys/kernel/debug",
		Source:      "tmpfs",
		Type:        "tmpfs",
		Options:     []string{"rw", "rprivate", "noexec", "nosuid", "nodev", "size=1m"},
	},
	// we don't virtualize tracefs; create a dummy one.
	specs.Mount{
		Destination: "/sys/kernel/tracing",
		Source:      "tmpfs",
		Type:        "tmpfs",
		Options:     []string{"rw", "rprivate", "noexec", "nosuid", "nodev", "size=1m"},
	},
	specs.Mount{
		Destination: "/proc",
		Source:      "proc",
		Type:        "proc",
		Options:     []string{"noexec", "nosuid", "nodev"},
	},
	specs.Mount{
		Destination: "/dev",
		Source:      "tmpfs",
		Type:        "tmpfs",
		Options:     []string{"nosuid", "strictatime", "mode=755", "size=65536k"},
	},
	//we don't yet support /dev/kmsg; create a dummy one
	specs.Mount{
		Destination: "/dev/kmsg",
		Source:      "/dev/null",
		Type:        "bind",
		Options:     []string{"rbind", "rprivate"},
	},
}

// system container mounts virtualized by sysbox-fs
var sysboxFsMounts = []specs.Mount{
	//
	// procfs mounts
	//
	specs.Mount{
		Destination: "/proc/sys",
		Source:      filepath.Join(SysboxFsDir, "proc/sys"),
		Type:        "bind",
		Options:     []string{"rbind", "rprivate"},
	},
	specs.Mount{
		Destination: "/proc/swaps",
		Source:      filepath.Join(SysboxFsDir, "proc/swaps"),
		Type:        "bind",
		Options:     []string{"rbind", "rprivate"},
	},
	specs.Mount{
		Destination: "/proc/uptime",
		Source:      filepath.Join(SysboxFsDir, "proc/uptime"),
		Type:        "bind",
		Options:     []string{"rbind", "rprivate"},
	},

	// XXX: In the future sysbox-fs will also virtualize the following

	// specs.Mount{
	// 	Destination: "/proc/cpuinfo",
	// 	Source:      filepath.Join(SysboxFsDir, "proc/cpuinfo"),
	// 	Type:        "bind",
	// 	Options:     []string{"rbind", "rprivate"},
	// },
	// specs.Mount{
	// 	Destination: "/proc/cgroups",
	// 	Source:      filepath.Join(SysboxFsDir, "proc/cgroups"),
	// 	Type:        "bind",
	// 	Options:     []string{"rbind", "rprivate"},
	// },
	// specs.Mount{
	// 	Destination: "/proc/devices",
	// 	Source:      filepath.Join(SysboxFsDir, "proc/devices"),
	// 	Type:        "bind",
	// 	Options:     []string{"rbind", "rprivate"},
	// },
	// specs.Mount{
	// 	Destination: "/proc/diskstats",
	// 	Source:      filepath.Join(SysboxFsDir, "proc/diskstats"),
	// 	Type:        "bind",
	// 	Options:     []string{"rbind", "rprivate"},
	// },
	// specs.Mount{
	// 	Destination: "/proc/loadavg",
	// 	Source:      filepath.Join(SysboxFsDir, "proc/loadavg"),
	// 	Type:        "bind",
	// 	Options:     []string{"rbind", "rprivate"},
	// },
	// specs.Mount{
	// 	Destination: "/proc/meminfo",
	// 	Source:      filepath.Join(SysboxFsDir, "proc/meminfo"),
	// 	Type:        "bind",
	// 	Options:     []string{"rbind", "rprivate"},
	// },
	// specs.Mount{
	// 	Destination: "/proc/pagetypeinfo",
	// 	Source:      filepath.Join(SysboxFsDir, "proc/pagetypeinfo"),
	// 	Type:        "bind",
	// 	Options:     []string{"rbind", "rprivate"},
	// },
	// specs.Mount{
	// 	Destination: "/proc/partitions",
	// 	Source:      filepath.Join(SysboxFsDir, "proc/partitions"),
	// 	Type:        "bind",
	// 	Options:     []string{"rbind", "rprivate"},
	// },
	// specs.Mount{
	// 	Destination: "/proc/stat",
	// 	Source:      filepath.Join(SysboxFsDir, "proc/stat"),
	// 	Type:        "bind",
	// 	Options:     []string{"rbind", "rprivate"},
	// },

	//
	// sysfs mounts
	//
	specs.Mount{
		Destination: "/sys/devices/virtual/dmi/id/product_uuid",
		Source:      filepath.Join(SysboxFsDir, "/sys/devices/virtual/dmi/id/product_uuid"),
		Type:        "bind",
		Options:     []string{"rbind", "rprivate"},
	},
	specs.Mount{
		Destination: "/sys/module/nf_conntrack/parameters/hashsize",
		Source:      filepath.Join(SysboxFsDir, "sys/module/nf_conntrack/parameters/hashsize"),
		Type:        "bind",
		Options:     []string{"rbind", "rprivate"},
	},
}

// sysbox's systemd mount requirements
var sysboxSystemdMounts = []specs.Mount{
	specs.Mount{
		Destination: "/run",
		Source:      "tmpfs",
		Type:        "tmpfs",
		Options:     []string{"rw", "rprivate", "nosuid", "nodev", "mode=755", "size=64m"},
	},
	specs.Mount{
		Destination: "/run/lock",
		Source:      "tmpfs",
		Type:        "tmpfs",
		Options:     []string{"rw", "rprivate", "noexec", "nosuid", "nodev", "size=4m"},
	},
}

// sysbox's systemd env-vars requirements
var sysboxSystemdEnvVars = []string{

	// Allow systemd to identify the virtualization mode to operate on (container
	// with user-namespace). See 'ConditionVirtualization' attribute here:
	// https://www.freedesktop.org/software/systemd/man/systemd.unit.html
	"container=private-users",
}

// sysboxRwPaths list the paths within the sys container's rootfs
// that must have read-write permission
var sysboxRwPaths = []string{
	"/proc",
	"/proc/sys",
}

// sysboxExposedPaths list the paths within the sys container's rootfs
// that must not be masked
var sysboxExposedPaths = []string{
	"/proc",
	"/proc/sys",

	// Some apps need these to be exposed (or more accurately need them to not be masked
	// via a bind-mount from /dev/null, as described in sysbox issue #511). It's not a
	// security concern to expose these in sys containers, as they are either not accesible
	// or don't provide meaningful info (due to the sys container's user-ns).
	"/proc/kcore",
	"/proc/kallsyms",
	"/proc/kmsg",
}

// sysboxSystemdExposedPaths list the paths within the sys container's rootfs
// that must not be masked when the sys container runs systemd
var sysboxSystemdExposedPaths = []string{
	"/run",
	"/run/lock",
	"/tmp",
	"/sys/kernel/config",
	"/sys/kernel/debug",
	"/sys/kernel/tracing",
}

// sysboxRwPaths list the paths within the sys container's rootfs
// that must have read-write permission
var sysboxSystemdRwPaths = []string{
	"/run",
	"/run/lock",
	"/tmp",
	"/sys/kernel/config",
	"/sys/kernel/debug",
	"/sys/kernel/tracing",
}

// linuxCaps is the full list of Linux capabilities
var linuxCaps = []string{
	"CAP_CHOWN",
	"CAP_DAC_OVERRIDE",
	"CAP_FSETID",
	"CAP_FOWNER",
	"CAP_MKNOD",
	"CAP_NET_RAW",
	"CAP_SETGID",
	"CAP_SETUID",
	"CAP_SETFCAP",
	"CAP_SETPCAP",
	"CAP_NET_BIND_SERVICE",
	"CAP_SYS_CHROOT",
	"CAP_KILL",
	"CAP_AUDIT_WRITE",
	"CAP_DAC_READ_SEARCH",
	"CAP_LINUX_IMMUTABLE",
	"CAP_NET_BROADCAST",
	"CAP_NET_ADMIN",
	"CAP_IPC_LOCK",
	"CAP_IPC_OWNER",
	"CAP_SYS_MODULE",
	"CAP_SYS_RAWIO",
	"CAP_SYS_PTRACE",
	"CAP_SYS_PACCT",
	"CAP_SYS_ADMIN",
	"CAP_SYS_BOOT",
	"CAP_SYS_NICE",
	"CAP_SYS_RESOURCE",
	"CAP_SYS_TIME",
	"CAP_SYS_TTY_CONFIG",
	"CAP_LEASE",
	"CAP_AUDIT_CONTROL",
	"CAP_MAC_OVERRIDE",
	"CAP_MAC_ADMIN",
	"CAP_SYSLOG",
	"CAP_WAKE_ALARM",
	"CAP_BLOCK_SUSPEND",
	"CAP_AUDIT_READ",
}

// cfgNamespaces checks that the namespace config has the minimum set
// of namespaces required and adds any missing namespaces to it
func cfgNamespaces(sysMgr *sysbox.Mgr, spec *specs.Spec) error {

	// user-ns and cgroup-ns are not required per the OCI spec, but we will add
	// them to the system container spec.
	var allNs = []string{"pid", "ipc", "uts", "mount", "network", "user", "cgroup"}
	var reqNs = []string{"pid", "ipc", "uts", "mount", "network"}

	allNsSet := mapset.NewSet()
	for _, ns := range allNs {
		allNsSet.Add(ns)
	}

	reqNsSet := mapset.NewSet()
	for _, ns := range reqNs {
		reqNsSet.Add(ns)
	}

	specNsSet := mapset.NewSet()
	for _, ns := range spec.Linux.Namespaces {
		specNsSet.Add(string(ns.Type))
	}

	if !reqNsSet.IsSubset(specNsSet) {
		return fmt.Errorf("container spec missing namespaces %v", reqNsSet.Difference(specNsSet))
	}

	addNsSet := allNsSet.Difference(specNsSet)
	for ns := range addNsSet.Iter() {
		str := fmt.Sprintf("%v", ns)
		newns := specs.LinuxNamespace{
			Type: specs.LinuxNamespaceType(str),
			Path: "",
		}
		spec.Linux.Namespaces = append(spec.Linux.Namespaces, newns)
		logrus.Debugf("added namespace %s to spec", ns)
	}

	// Check if we have a sysbox-mgr override for the container's user-ns
	if sysMgr.Enabled() {
		if sysMgr.Config.Userns != "" {
			updatedNs := []specs.LinuxNamespace{}

			for _, ns := range spec.Linux.Namespaces {
				if ns.Type == specs.UserNamespace {
					ns.Path = sysMgr.Config.Userns
				}
				updatedNs = append(updatedNs, ns)
			}

			spec.Linux.Namespaces = updatedNs
		}
	}

	return nil
}

// allocIDMappings performs uid and gid allocation for the system container
func allocIDMappings(sysMgr *sysbox.Mgr, spec *specs.Spec) error {
	var uid, gid uint32
	var err error

	if sysMgr.Enabled() {
		uid, gid, err = sysMgr.ReqSubid(IdRangeMin)
		if err != nil {
			return fmt.Errorf("subid allocation failed: %v", err)
		}
	} else {
		uid = defaultUid
		gid = defaultGid
	}

	uidMap := specs.LinuxIDMapping{
		ContainerID: 0,
		HostID:      uid,
		Size:        IdRangeMin,
	}

	gidMap := specs.LinuxIDMapping{
		ContainerID: 0,
		HostID:      gid,
		Size:        IdRangeMin,
	}

	spec.Linux.UIDMappings = append(spec.Linux.UIDMappings, uidMap)
	spec.Linux.GIDMappings = append(spec.Linux.GIDMappings, gidMap)

	return nil
}

// validateIDMappings checks if the spec's user namespace uid and gid mappings meet
// sysbox-runc requirements.
func validateIDMappings(spec *specs.Spec) error {
	var err error

	if len(spec.Linux.UIDMappings) == 0 || len(spec.Linux.GIDMappings) == 0 {
		return fmt.Errorf("detected missing user-ns UID and/or GID mappings")
	}

	// Sysbox requires that the container uid & gid mappings map a continuous
	// range of container IDs to host IDs. This is a requirement implicitly
	// imposed by Sysbox's usage of shiftfs. The call to mergeIDmappings ensures
	// this is the case and returns a single ID mapping range in case the
	// container's spec gave us a continuous mapping in multiple continuous
	// sub-ranges.

	spec.Linux.UIDMappings, err = mergeIDMappings(spec.Linux.UIDMappings)
	if err != nil {
		return err
	}

	spec.Linux.GIDMappings, err = mergeIDMappings(spec.Linux.GIDMappings)
	if err != nil {
		return err
	}

	uidMap := spec.Linux.UIDMappings[0]
	gidMap := spec.Linux.GIDMappings[0]

	if uidMap.ContainerID != 0 || uidMap.Size < IdRangeMin {
		return fmt.Errorf("uid mapping range must specify a container with at least %d uids starting at uid 0; found %v",
			IdRangeMin, uidMap)
	}

	if gidMap.ContainerID != 0 || gidMap.Size < IdRangeMin {
		return fmt.Errorf("gid mapping range must specify a container with at least %d gids starting at gid 0; found %v",
			IdRangeMin, gidMap)
	}

	if uidMap.HostID != gidMap.HostID {
		return fmt.Errorf("detecting non-matching uid & gid mappings; found uid = %v, gid = %d",
			uidMap, gidMap)
	}

	if uidMap.HostID == 0 {
		return fmt.Errorf("detected user-ns uid mapping to host ID 0 (%v); this breaks container isolation",
			uidMap)
	}

	if gidMap.HostID == 0 {
		return fmt.Errorf("detected user-ns gid mapping to host ID 0 (%v); this breaks container isolation",
			uidMap)
	}

	return nil
}

// cfgIDMappings checks if the uid/gid mappings are present and valid; if they are not
// present, it allocates them.
func cfgIDMappings(sysMgr *sysbox.Mgr, spec *specs.Spec) error {

	// Honor user-ns uid & gid mapping spec overrides from sysbox-mgr; this occur
	// when a container shares the same userns and netns of another container (i.e.,
	// they must also share the mappings).
	if sysMgr.Enabled() {
		if len(sysMgr.Config.UidMappings) > 0 {
			spec.Linux.UIDMappings = sysMgr.Config.UidMappings
		}
		if len(sysMgr.Config.GidMappings) > 0 {
			spec.Linux.GIDMappings = sysMgr.Config.GidMappings
		}
	}

	// If no mappings are present, let's allocate some.
	if len(spec.Linux.UIDMappings) == 0 && len(spec.Linux.GIDMappings) == 0 {
		return allocIDMappings(sysMgr, spec)
	}

	return validateIDMappings(spec)
}

// cfgCapabilities sets the capabilities for the process in the system container
func cfgCapabilities(p *specs.Process) {
	caps := p.Capabilities
	uid := p.User.UID

	noCaps := []string{}

	if uid == 0 {
		// init processes owned by root have all capabilities
		caps.Bounding = linuxCaps
		caps.Effective = linuxCaps
		caps.Inheritable = linuxCaps
		caps.Permitted = linuxCaps
		caps.Ambient = linuxCaps
	} else {
		// init processes owned by others have all caps disabled and the bounding caps all
		// set (just as in a regular host)
		caps.Bounding = linuxCaps
		caps.Effective = noCaps
		caps.Inheritable = noCaps
		caps.Permitted = noCaps
		caps.Ambient = noCaps
	}
}

// cfgMaskedPaths removes from the container's config any masked paths for which
// sysbox-fs will handle accesses.
func cfgMaskedPaths(spec *specs.Spec) {
	if systemdInit(spec.Process) {
		spec.Linux.MaskedPaths = utils.StringSliceRemove(spec.Linux.MaskedPaths, sysboxSystemdExposedPaths)
	}
	spec.Linux.MaskedPaths = utils.StringSliceRemove(spec.Linux.MaskedPaths, sysboxExposedPaths)
}

// cfgReadonlyPaths removes from the container's config any read-only paths
// that must be read-write in the system container
func cfgReadonlyPaths(spec *specs.Spec) {
	if systemdInit(spec.Process) {
		spec.Linux.ReadonlyPaths = utils.StringSliceRemove(spec.Linux.ReadonlyPaths, sysboxSystemdRwPaths)
	}
	spec.Linux.ReadonlyPaths = utils.StringSliceRemove(spec.Linux.ReadonlyPaths, sysboxRwPaths)
}

// cfgMounts configures the system container mounts
func cfgMounts(spec *specs.Spec, sysMgr *sysbox.Mgr, sysFs *sysbox.Fs, uidShiftRootfs bool) error {

	cfgSysboxMounts(spec)

	if sysFs.Enabled() {
		cfgSysboxFsMounts(spec, sysFs)
	}

	if sysMgr.Enabled() {
		if err := sysMgrSetupMounts(sysMgr, spec, uidShiftRootfs); err != nil {
			return err
		}
	}

	if systemdInit(spec.Process) {
		cfgSystemdMounts(spec)
	}

	sortMounts(spec)

	return nil
}

// cfgSysboxMounts adds Sysbox required mounts to the sys container's spec; if the spec
// has conflicting mounts, these are replaced with Sysbox's mounts.
func cfgSysboxMounts(spec *specs.Spec) {

	// Disallow mounts under the container's /sys/fs/cgroup/* (i.e., Sysbox sets those up)
	var cgroupMounts = []specs.Mount{
		specs.Mount{
			Destination: "/sys/fs/cgroup/",
		},
	}

	spec.Mounts = utils.MountSliceRemove(spec.Mounts, cgroupMounts, func(m1, m2 specs.Mount) bool {
		return strings.HasPrefix(m1.Destination, m2.Destination)
	})

	// Remove other conflicting mounts
	spec.Mounts = utils.MountSliceRemove(spec.Mounts, sysboxMounts, func(m1, m2 specs.Mount) bool {
		return m1.Destination == m2.Destination
	})

	// If the container's rootfs is read-only, then sysbox mounts of /sys and
	// below should also be read-only.
	if spec.Root.Readonly {
		tmpMounts := []specs.Mount{}
		rwOpt := []string{"rw"}
		for _, m := range sysboxMounts {
			if strings.HasPrefix(m.Destination, "/sys") {
				m.Options = utils.StringSliceRemove(m.Options, rwOpt)
				m.Options = append(m.Options, "ro")
			}
			tmpMounts = append(tmpMounts, m)
		}
		sysboxMounts = tmpMounts
	}

	// Add sysbox mounts
	spec.Mounts = append(spec.Mounts, sysboxMounts...)
}

// cfgSysboxFsMounts adds the sysbox-fs mounts to the containers config.
func cfgSysboxFsMounts(spec *specs.Spec, sysFs *sysbox.Fs) {
	spec.Mounts = utils.MountSliceRemove(spec.Mounts, sysboxFsMounts, func(m1, m2 specs.Mount) bool {
		return m1.Destination == m2.Destination
	})

	// Adjust sysboxFsMounts path attending to container-id value.
	cntrMountpoint := filepath.Join(sysFs.Mountpoint, sysFs.Id)

	for i := range sysboxFsMounts {
		sysboxFsMounts[i].Source =
			strings.Replace(
				sysboxFsMounts[i].Source,
				SysboxFsDir,
				cntrMountpoint,
				1,
			)
	}

	SysboxFsDir = sysFs.Mountpoint

	// If the spec indicates a read-only rootfs, the sysbox-fs mounts should also
	// be read-only. However, we don't mark them read-only here explicitly, so
	// that they are initially mounted read-write while setting up the container.
	// This is needed because the setup process may need to write to some of
	// these mounts (e.g., writes to /proc/sys during networking setup). Instead,
	// we add the mounts to the "readonly" paths list, so that they will be
	// remounted to read-only after the container setup completes, right before
	// starting the container's init process.
	if spec.Root.Readonly {
		for _, m := range sysboxFsMounts {
			spec.Linux.ReadonlyPaths = append(spec.Linux.ReadonlyPaths, m.Destination)
		}
	}

	spec.Mounts = append(spec.Mounts, sysboxFsMounts...)
}

// cfgSystemdMounts adds systemd related mounts to the spec
func cfgSystemdMounts(spec *specs.Spec) {

	// For sys containers with systemd inside, sysbox mounts tmpfs over certain directories
	// of the container (this is a systemd requirement). However, if the container spec
	// already has tmpfs mounts over any of these directories, we honor the spec mounts
	// (i.e., these override the sysbox mount).

	spec.Mounts = utils.MountSliceRemove(spec.Mounts, sysboxSystemdMounts, func(m1, m2 specs.Mount) bool {
		return m1.Destination == m2.Destination && m1.Type != "tmpfs"
	})

	sysboxSystemdMounts = utils.MountSliceRemove(sysboxSystemdMounts, spec.Mounts, func(m1, m2 specs.Mount) bool {
		return m1.Destination == m2.Destination && m2.Type == "tmpfs"
	})

	spec.Mounts = append(spec.Mounts, sysboxSystemdMounts...)
}

// sysMgrSetupMounts requests the sysbox-mgr to setup special sys container mounts.
func sysMgrSetupMounts(mgr *sysbox.Mgr, spec *specs.Spec, uidShiftRootfs bool) error {

	// These directories in the sys container are bind-mounted from host dirs managed by sysbox-mgr
	specialDir := map[string]ipcLib.MntKind{
		"/var/lib/docker":      ipcLib.MntVarLibDocker,
		"/var/lib/kubelet":     ipcLib.MntVarLibKubelet,
		"/var/lib/rancher/k3s": ipcLib.MntVarLibK3s,
		"/var/lib/containerd/io.containerd.snapshotter.v1.overlayfs": ipcLib.MntVarLibContainerdOvfs,
	}

	uid := spec.Linux.UIDMappings[0].HostID
	gid := spec.Linux.GIDMappings[0].HostID

	// If the spec has a bind-mount over one of the special dirs, ask the
	// sysbox-mgr to prepare the mount source (e.g., chown files to match the
	// container host uid & gid).
	prepList := []ipcLib.MountPrepInfo{}

	for i := len(spec.Mounts) - 1; i >= 0; i-- {

		m := spec.Mounts[i]
		_, isSpecialDir := specialDir[m.Destination]

		if m.Type == "bind" && isSpecialDir {
			info := ipcLib.MountPrepInfo{
				Source:    m.Source,
				Exclusive: true,
			}

			prepList = append(prepList, info)
			delete(specialDir, m.Destination)
		}
	}

	if len(prepList) > 0 {
		if err := mgr.PrepMounts(uid, gid, prepList); err != nil {
			return err
		}
	}

	// Otherwise, add the special dir to the list of mounts that we will request
	// sysbox-mgr to setup
	reqList := []ipcLib.MountReqInfo{}
	for dest, kind := range specialDir {
		info := ipcLib.MountReqInfo{
			Kind: kind,
			Dest: dest,
		}
		reqList = append(reqList, info)
	}

	// sysbox-mgr will setup host dirs to back the mounts in the
	// request list; it will also send us any other mounts it needs.
	rootPath, err := filepath.Abs(spec.Root.Path)
	if err != nil {
		return err
	}

	m, err := mgr.ReqMounts(rootPath, uid, gid, uidShiftRootfs, reqList)
	if err != nil {
		return err
	}

	// If any sysbox-mgr mounts conflict with any in the spec (i.e.,
	// same dest), prioritize the spec ones
	mounts := utils.MountSliceRemove(m, spec.Mounts, func(m1, m2 specs.Mount) bool {
		return m1.Destination == m2.Destination
	})

	// If the spec indicates a read-only rootfs, the sysbox-mgr mounts should
	// also be read-only.
	if spec.Root.Readonly {
		tmpMounts := []specs.Mount{}
		rwOpt := []string{"rw"}
		for _, m := range mounts {
			m.Options = utils.StringSliceRemove(m.Options, rwOpt)
			m.Options = append(m.Options, "ro")
			tmpMounts = append(tmpMounts, m)
		}
		mounts = tmpMounts
	}

	spec.Mounts = append(spec.Mounts, mounts...)

	return nil
}

// checkSpec performs some basic checks on the system container's spec
func checkSpec(spec *specs.Spec) error {

	if spec.Root == nil || spec.Linux == nil {
		return fmt.Errorf("not a linux container spec")
	}

	// Ensure the container's network ns is not shared with the host
	for _, ns := range spec.Linux.Namespaces {
		if ns.Type == specs.NetworkNamespace && ns.Path != "" {
			var st1, st2 unix.Stat_t

			if err := unix.Stat("/proc/self/ns/net", &st1); err != nil {
				return fmt.Errorf("unable to stat sysbox's network namespace: %s", err)
			}
			if err := unix.Stat(ns.Path, &st2); err != nil {
				return fmt.Errorf("unable to stat %q: %s", ns.Path, err)
			}

			if (st1.Dev == st2.Dev) && (st1.Ino == st2.Ino) {
				return fmt.Errorf("sysbox containers can't share a network namespace with the host (because they use the linux user-namespace for isolation)")
			}

			break
		}
	}

	return nil
}

func cfgOomScoreAdj(spec *specs.Spec) {

	// For sys containers we don't allow -1000 for the OOM score value, as this
	// is not supported from within a user-ns.

	if spec.Process.OOMScoreAdj != nil {
		if *spec.Process.OOMScoreAdj < -999 {
			*spec.Process.OOMScoreAdj = -999
		}
	}
}

// cfgSeccomp configures the system container's seccomp settings.
func cfgSeccomp(seccomp *specs.LinuxSeccomp) error {

	if seccomp == nil {
		return nil
	}

	supportedArch := false
	for _, arch := range seccomp.Architectures {
		if arch == specs.ArchX86_64 {
			supportedArch = true
		}
	}
	if !supportedArch {
		return nil
	}

	// we don't yet support specs with default trap, trace, or log actions
	if seccomp.DefaultAction != specs.ActAllow &&
		seccomp.DefaultAction != specs.ActErrno &&
		seccomp.DefaultAction != specs.ActKill {
		return fmt.Errorf("spec seccomp default actions other than allow, errno, and kill are not supported")
	}

	// categorize syscalls per seccomp actions
	allowSet := mapset.NewSet()
	errnoSet := mapset.NewSet()
	killSet := mapset.NewSet()

	for _, syscall := range seccomp.Syscalls {
		for _, name := range syscall.Names {
			switch syscall.Action {
			case specs.ActAllow:
				allowSet.Add(name)
			case specs.ActErrno:
				errnoSet.Add(name)
			case specs.ActKill:
				killSet.Add(name)
			}
		}
	}

	// convert sys container syscall whitelist to a set
	syscontAllowSet := mapset.NewSet()
	for _, sc := range syscontSyscallWhitelist {
		syscontAllowSet.Add(sc)
	}

	// seccomp syscall list may be a whitelist or blacklist
	whitelist := (seccomp.DefaultAction == specs.ActErrno ||
		seccomp.DefaultAction == specs.ActKill)

	// diffset is the set of syscalls that needs adding (for whitelist) or removing (for blacklist)
	diffSet := mapset.NewSet()
	if whitelist {
		diffSet = syscontAllowSet.Difference(allowSet)
	} else {
		disallowSet := errnoSet.Union(killSet)
		diffSet = disallowSet.Difference(syscontAllowSet)
	}

	if whitelist {
		// add the diffset to the whitelist
		for syscallName := range diffSet.Iter() {
			str := fmt.Sprintf("%v", syscallName)
			sc := specs.LinuxSyscall{
				Names:  []string{str},
				Action: specs.ActAllow,
			}
			seccomp.Syscalls = append(seccomp.Syscalls, sc)
		}

		logrus.Debugf("added syscalls to seccomp profile: %v", diffSet)

	} else {
		// remove the diffset from the blacklist
		var newSyscalls []specs.LinuxSyscall
		for _, sc := range seccomp.Syscalls {
			for i, scName := range sc.Names {
				if diffSet.Contains(scName) {
					// Remove this syscall
					sc.Names = append(sc.Names[:i], sc.Names[i+1:]...)
				}
			}
			if sc.Names != nil {
				newSyscalls = append(newSyscalls, sc)
			}
		}
		seccomp.Syscalls = newSyscalls

		logrus.Debugf("removed syscalls from seccomp profile: %v", diffSet)
	}

	if whitelist {
		// Remove argument restrictions on syscalls (except those for which we
		// allow such restrictions).
		for i, syscall := range seccomp.Syscalls {
			for _, name := range syscall.Names {
				if !utils.StringSliceContains(syscontSyscallAllowRestrList, name) {
					seccomp.Syscalls[i].Args = nil
				}
			}
		}
	}

	return nil
}

// cfgAppArmor sets up the apparmor config for sys containers
func cfgAppArmor(p *specs.Process) error {

	// The default docker profile is too restrictive for sys containers (e.g., preveting
	// mounts, write access to /proc/sys/*, etc). For now, we simply ignore any apparmor
	// profile in the container's config.
	//
	// TODO: In the near future, we should develop an apparmor profile for sys-containers,
	// and have sysbox-mgr load it to the kernel (if apparmor is enabled on the system)
	// and then configure the container to use that profile here.

	p.ApparmorProfile = ""
	return nil
}

// Configure environment variables required for systemd
func cfgSystemdEnv(p *specs.Process) {

	p.Env = utils.StringSliceRemoveMatch(p.Env, func(specEnvVar string) bool {
		name, _, err := utils.GetEnvVarInfo(specEnvVar)
		if err != nil {
			return false
		}
		for _, sysboxSysdEnvVar := range sysboxSystemdEnvVars {
			sname, _, err := utils.GetEnvVarInfo(sysboxSysdEnvVar)
			if err == nil && name == sname {
				return true
			}
		}
		return false
	})

	p.Env = append(p.Env, sysboxSystemdEnvVars...)
}

// systemdInit returns true if the sys container is running systemd
func systemdInit(p *specs.Process) bool {
	return p.Args[0] == "/sbin/init"
}

// Configure the container's process spec for system containers
func ConvertProcessSpec(p *specs.Process) error {

	cfgCapabilities(p)

	if err := cfgAppArmor(p); err != nil {
		return fmt.Errorf("failed to configure AppArmor profile: %v", err)
	}

	if systemdInit(p) {
		cfgSystemdEnv(p)
	}

	return nil
}

// ConvertSpec converts the given container spec to a system container spec.
func ConvertSpec(context *cli.Context, sysMgr *sysbox.Mgr, sysFs *sysbox.Fs, spec *specs.Spec) (bool, bool, error) {

	if err := checkSpec(spec); err != nil {
		return false, false, fmt.Errorf("invalid or unsupported container spec: %v", err)
	}

	if err := cfgNamespaces(sysMgr, spec); err != nil {
		return false, false, fmt.Errorf("invalid namespace config: %v", err)
	}

	if err := cfgIDMappings(sysMgr, spec); err != nil {
		return false, false, fmt.Errorf("invalid user/group ID config: %v", err)
	}

	// Must do this after cfgIDMappings()
	uidShiftSupported, uidShiftRootfs, err := sysbox.CheckUidShifting(spec)
	if err != nil {
		return false, false, err
	}

	if err := cfgMounts(spec, sysMgr, sysFs, uidShiftRootfs); err != nil {
		return false, false, fmt.Errorf("invalid mount config: %v", err)
	}

	cfgMaskedPaths(spec)
	cfgReadonlyPaths(spec)
	cfgOomScoreAdj(spec)

	if err := cfgSeccomp(spec.Linux.Seccomp); err != nil {
		return false, false, fmt.Errorf("failed to configure seccomp: %v", err)
	}

	if err := ConvertProcessSpec(spec.Process); err != nil {
		return false, false, fmt.Errorf("failed to configure process spec: %v", err)
	}

	return uidShiftSupported, uidShiftRootfs, nil
}
