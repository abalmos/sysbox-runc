//
// Copyright: (C) 2019 Nestybox Inc.  All rights reserved.
//

// +build linux

package syscont

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	mapset "github.com/deckarep/golang-set"
	ipcLib "github.com/nestybox/sysbox-ipc/sysboxMgrLib"
	"github.com/opencontainers/runc/libsysbox/sysbox"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli"
)

// Exported
const (
	SysboxFsDir string = "/var/lib/sysboxfs"
	IdRangeMin  uint32 = 65536
)

// Internal
const (
	defaultUid uint32 = 231072
	defaultGid uint32 = 231072
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
}

// sysbox's systemd mount requirements
var sysboxSystemdMounts = []specs.Mount{
	specs.Mount{
		Destination: "/run",
		Source:      "tmpfs",
		Type:        "tmpfs",
		Options:     []string{"rw", "rprivate", "noexec", "nosuid", "nodev", "tmpcopyup", "size=64m"},
	},
	specs.Mount{
		Destination: "/run/lock",
		Source:      "tmpfs",
		Type:        "tmpfs",
		Options:     []string{"rw", "rprivate", "noexec", "nosuid", "nodev", "tmpcopyup", "size=4m"},
	},
	specs.Mount{
		Destination: "/tmp",
		Source:      "tmpfs",
		Type:        "tmpfs",
		Options:     []string{"rw", "rprivate", "noexec", "nosuid", "nodev", "tmpcopyup", "size=64m"},
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
}

// sysboxSystemdExposedPaths list the paths within the sys container's rootfs
// that must not be masked when the sys container runs systemd
var sysboxSystemdExposedPaths = []string{
	"/run",
	"/run/lock",
	"/tmp",
	"/sys/kernel/config",
	"/sys/kernel/debug",
}

// sysboxRwPaths list the paths within the sys container's rootfs
// that must have read-write permission
var sysboxSystemdRwPaths = []string{
	"/run",
	"/run/lock",
	"/tmp",
	"/sys/kernel/config",
	"/sys/kernel/debug",
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
func cfgNamespaces(spec *specs.Spec) error {

	// user-ns and cgroup-ns are not required; but we will add them to the spec.
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

// validateIDMappings checks if the spec's user namespace uid and gid mappings meet sysbox-runc requirements
func validateIDMappings(spec *specs.Spec) error {

	if len(spec.Linux.UIDMappings) != 1 {
		return fmt.Errorf("sysbox-runc requires user namespace uid mapping array have one element; found %v", spec.Linux.UIDMappings)
	}

	if len(spec.Linux.GIDMappings) != 1 {
		return fmt.Errorf("sysbox-runc requires user namespace gid mapping array have one element; found %v", spec.Linux.GIDMappings)
	}

	uidMap := spec.Linux.UIDMappings[0]
	if uidMap.ContainerID != 0 || uidMap.Size < IdRangeMin {
		return fmt.Errorf("sysbox-runc requires uid mapping specify a container with at least %d uids starting at uid 0; found %v", IdRangeMin, uidMap)
	}

	gidMap := spec.Linux.GIDMappings[0]
	if gidMap.ContainerID != 0 || gidMap.Size < IdRangeMin {
		return fmt.Errorf("sysbox-runc requires gid mapping specify a container with at least %d gids starting at gid 0; found %v", IdRangeMin, gidMap)
	}

	if uidMap.HostID != gidMap.HostID {
		return fmt.Errorf("sysbox-runc requires matching uid & gid mappings; found uid = %v, gid = %d", uidMap, gidMap)
	}

	return nil
}

// cfgIDMappings checks if the uid/gid mappings are present and valid; if they are not
// present, it allocates them. Note that we don't disallow mappings that map to the host
// root UID (i.e., identity-mappings); some runc tests use such mappings.
func cfgIDMappings(sysMgr *sysbox.Mgr, spec *specs.Spec) error {
	if len(spec.Linux.UIDMappings) == 0 && len(spec.Linux.GIDMappings) == 0 {
		return allocIDMappings(sysMgr, spec)
	}
	return validateIDMappings(spec)
}

// cfgCapabilities sets the capabilities for the process in the system container
func cfgCapabilities(p *specs.Process) {
	caps := p.Capabilities
	uid := p.User.UID

	// In a sys container, init procesees owned by root have all capabilities
	if uid == 0 {
		caps.Bounding = linuxCaps
		caps.Effective = linuxCaps
		caps.Inheritable = linuxCaps
		caps.Permitted = linuxCaps
		caps.Ambient = linuxCaps
	}
}

// cfgMaskedPaths removes from the container's config any masked paths for which
// sysbox-fs will handle accesses.
func cfgMaskedPaths(spec *specs.Spec) {
	if systemdInit(spec.Process) {
		spec.Linux.MaskedPaths = stringSliceRemove(spec.Linux.MaskedPaths, sysboxSystemdExposedPaths)
	}
	spec.Linux.MaskedPaths = stringSliceRemove(spec.Linux.MaskedPaths, sysboxExposedPaths)
}

// cfgReadonlyPaths removes from the container's config any read-only paths
// that must be read-write in the system container
func cfgReadonlyPaths(spec *specs.Spec) {
	if systemdInit(spec.Process) {
		spec.Linux.ReadonlyPaths = stringSliceRemove(spec.Linux.ReadonlyPaths, sysboxSystemdRwPaths)
	}
	spec.Linux.ReadonlyPaths = stringSliceRemove(spec.Linux.ReadonlyPaths, sysboxRwPaths)
}

// cfgMounts configures the system container mounts
func cfgMounts(spec *specs.Spec, sysMgr *sysbox.Mgr, sysFs *sysbox.Fs, shiftUids bool) error {

	cfgSysboxMounts(spec)

	if err := cfgLinuxHeadersMount(spec); err != nil {
		return err
	}

	if err := cfgLibModMount(spec); err != nil {
		return err
	}

	if sysFs.Enabled() {
		cfgSysboxFsMounts(spec)
	}

	if sysMgr.Enabled() {
		if err := sysMgrSetupMounts(sysMgr, spec, shiftUids); err != nil {
			return err
		}
	}

	if systemdInit(spec.Process) {
		cfgSystemdMounts(spec)
	}

	sortMounts(spec)
	return nil
}

// cfgSysboxMounts adds sysbox generic mounts to the sys container's spec.
func cfgSysboxMounts(spec *specs.Spec) {
	spec.Mounts = mountSliceRemove(spec.Mounts, sysboxMounts, func(m1, m2 specs.Mount) bool {
		return m1.Destination == m2.Destination
	})
	spec.Mounts = append(spec.Mounts, sysboxMounts...)
}

// cfgSysboxFsMounts adds the sysbox-fs mounts to the containers config.
func cfgSysboxFsMounts(spec *specs.Spec) {
	spec.Mounts = mountSliceRemove(spec.Mounts, sysboxFsMounts, func(m1, m2 specs.Mount) bool {
		return m1.Destination == m2.Destination
	})
	spec.Mounts = append(spec.Mounts, sysboxFsMounts...)
}

// cfgSystemdMounts adds systemd related mounts to the spec
func cfgSystemdMounts(spec *specs.Spec) {
	spec.Mounts = mountSliceRemove(spec.Mounts, sysboxSystemdMounts, func(m1, m2 specs.Mount) bool {
		return m1.Destination == m2.Destination
	})
	spec.Mounts = append(spec.Mounts, sysboxSystemdMounts...)
}

// cfgLibModMount configures the sys container spec with a read-only mount of the host's
// kernel modules directory (/lib/modules/<kernel-release>). This allows system container
// processes to verify the presence of modules via modprobe. System apps such as Docker
// and K8s do this. Note that this does not imply module loading/unloading is supported in
// a system container (it's not). It merely lets processes check if a module is loaded.
func cfgLibModMount(spec *specs.Spec) error {

	kernelRel, err := sysbox.GetKernelRelease()
	if err != nil {
		return err
	}

	path := filepath.Join("/lib/modules/", kernelRel)
	if _, err := os.Stat(path); err != nil {
		return err
	}

	mounts := []specs.Mount{}

	// don't follow symlinks as they normally point to the linux headers which we mount in cfgLinuxHeadersMount
	mounts, err = createMountSpec(path, path, "bind", []string{"ro", "rbind", "rprivate"}, true, []string{"/usr/src"})
	if err != nil {
		return fmt.Errorf("failed to create mount spec for linux modules at %s: %v", path, err)
	}

	// if the mounts conflict with any in the spec (i.e., same dest), prioritize the spec ones
	mounts = mountSliceRemove(mounts, spec.Mounts, func(m1, m2 specs.Mount) bool {
		return m1.Destination == m2.Destination
	})

	spec.Mounts = append(spec.Mounts, mounts...)
	return nil
}

// cfgLinuxHeadersMount configures the sys container spec with a read-only mount of the
// host's linux kernel headers. This is needed to build or run apps that interact with the
// Linux kernel directly within a sys container.
func cfgLinuxHeadersMount(spec *specs.Spec) error {

	kernelRel, err := sysbox.GetKernelRelease()
	if err != nil {
		return err
	}

	kernelHdr := "linux-headers-" + kernelRel

	path := filepath.Join("/usr/src/", kernelHdr)
	if _, err := os.Stat(path); err != nil {
		return err
	}

	mounts := []specs.Mount{}

	// follow symlinks as some distros (e.g., Ubuntu) heavily symlink the linux header directory
	mounts, err = createMountSpec(path, path, "bind", []string{"ro", "rbind", "rprivate"}, true, []string{"/usr/src"})
	if err != nil {
		return fmt.Errorf("failed to create mount spec for linux headers at %s: %v", path, err)
	}

	// if the mounts conflict with any in the spec (i.e., same dest), prioritize the spec ones
	mounts = mountSliceRemove(mounts, spec.Mounts, func(m1, m2 specs.Mount) bool {
		return m1.Destination == m2.Destination
	})

	spec.Mounts = append(spec.Mounts, mounts...)
	return nil
}

// sysMgrSetupMounts requests the sysbox-mgr to setup special sys container mounts
func sysMgrSetupMounts(mgr *sysbox.Mgr, spec *specs.Spec, shiftUids bool) error {

	// This directories in the sys container are backed by mounts handled by sysbox-mgr
	specialDir := map[string]bool{
		"/var/lib/docker":  true,
		"/var/lib/kubelet": true,
	}

	uid := spec.Linux.UIDMappings[0].HostID
	gid := spec.Linux.GIDMappings[0].HostID

	// if the spec has a bind-mount over one of the special dirs, ask the sysbox-mgr to
	// prepare the mount source
	prepList := []ipcLib.MountPrepInfo{}
	for i := len(spec.Mounts) - 1; i >= 0; i-- {
		m := spec.Mounts[i]
		if m.Type == "bind" && specialDir[m.Destination] {
			info := ipcLib.MountPrepInfo{
				Source:    m.Source,
				Exclusive: true,
			}
			prepList = append(prepList, info)
			delete(specialDir, m.Destination)
		}
	}

	if len(prepList) > 0 {
		if err := mgr.PrepMounts(uid, gid, shiftUids, prepList); err != nil {
			return err
		}
	}

	// otherwise, request sysbox-mgr to create a bind-mount to back the special dir
	reqList := []ipcLib.MountReqInfo{}
	for m := range specialDir {
		info := ipcLib.MountReqInfo{
			Dest: m,
		}
		reqList = append(reqList, info)
	}

	if len(reqList) > 0 {
		m, err := mgr.ReqMounts(spec.Root.Path, uid, gid, shiftUids, reqList)
		if err != nil {
			return err
		}
		spec.Mounts = append(spec.Mounts, m...)
	}

	return nil
}

// checkSpec performs some basic checks on the system container's spec
func checkSpec(spec *specs.Spec) error {

	if spec.Root == nil || spec.Linux == nil {
		return fmt.Errorf("not a linux container spec")
	}

	if spec.Root.Readonly {
		return fmt.Errorf("root path must be read-write but it's set to read-only")
	}

	return nil
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

// needUidShiftOnRootfs checks if uid/gid shifting is required to run the system container.
func needUidShiftOnRootfs(spec *specs.Spec) (bool, error) {
	var hostUidMap, hostGidMap uint32

	// the uid map is assumed to be present
	for _, mapping := range spec.Linux.UIDMappings {
		if mapping.ContainerID == 0 {
			hostUidMap = mapping.HostID
		}
	}

	// the gid map is assumed to be present
	for _, mapping := range spec.Linux.GIDMappings {
		if mapping.ContainerID == 0 {
			hostGidMap = mapping.HostID
		}
	}

	// find the rootfs owner
	rootfs := spec.Root.Path

	fi, err := os.Stat(rootfs)
	if err != nil {
		return false, err
	}

	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return false, fmt.Errorf("failed to convert to syscall.Stat_t")
	}

	rootfsUid := st.Uid
	rootfsGid := st.Gid

	// Use shifting when the rootfs is owned by true root, the containers uid/gid root
	// mapping don't match the container's rootfs owner, and the host ID for the uid and
	// gid mappings is the same.

	if rootfsUid == 0 && rootfsGid == 0 &&
		hostUidMap != rootfsUid && hostGidMap != rootfsGid &&
		hostUidMap == hostGidMap {
		return true, nil
	}

	return false, nil
}

// Configure environment variables required for systemd
func cfgSystemdEnv(p *specs.Process) {

	p.Env = stringSliceRemoveMatch(p.Env, func(specEnvVar string) bool {
		name, _, err := getEnvVarInfo(specEnvVar)
		if err != nil {
			return false
		}
		for _, sysboxSysdEnvVar := range sysboxSystemdEnvVars {
			sname, _, err := getEnvVarInfo(sysboxSysdEnvVar)
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
func ConvertSpec(context *cli.Context, sysMgr *sysbox.Mgr, sysFs *sysbox.Fs, spec *specs.Spec) (bool, error) {

	if err := checkSpec(spec); err != nil {
		return false, fmt.Errorf("invalid or unsupported container spec: %v", err)
	}

	if err := cfgNamespaces(spec); err != nil {
		return false, fmt.Errorf("invalid namespace config: %v", err)
	}

	if err := cfgIDMappings(sysMgr, spec); err != nil {
		return false, fmt.Errorf("invalid user/group ID config: %v", err)
	}

	// Must be done after cfgIDMappings()
	shiftUids, err := needUidShiftOnRootfs(spec)
	if err != nil {
		return false, fmt.Errorf("error while checking for uid-shifting need: %v", err)
	}

	if err := cfgMounts(spec, sysMgr, sysFs, shiftUids); err != nil {
		return false, fmt.Errorf("invalid mount config: %v", err)
	}

	cfgMaskedPaths(spec)
	cfgReadonlyPaths(spec)

	if err := cfgSeccomp(spec.Linux.Seccomp); err != nil {
		return false, fmt.Errorf("failed to configure seccomp: %v", err)
	}

	if err := ConvertProcessSpec(spec.Process); err != nil {
		return false, fmt.Errorf("failed to configure process spec: %v", err)
	}

	return shiftUids, nil
}
