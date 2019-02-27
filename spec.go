// +build linux

package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"

	"nestybox/sysvisor-runc/libcontainer/configs"
	"nestybox/sysvisor-runc/libsyscontainer/syscontSpec"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/urfave/cli"
)

var specCommand = cli.Command{
	Name:      "spec",
	Usage:     "create a new system container specification file",
	ArgsUsage: "",
	Description: `The spec command creates the new system container specification file named "` + specConfig + `" for
the bundle.

The spec generated is just a starter file. Editing of the spec is required to
achieve desired results.

System containers always use the user namespace; the user ID and group ID mappings
generated by this command correspond to the /etc/subuid and /etc/subgid ranges
associated with the user and group owner of the bundle.

When starting a container through sysvisor-runc, sysvior-runc needs root privilege. If not
already running as root, you can use sudo to give sysvisor-runc root privilege. For
example: "sudo sysvisor-runc start syscont1" will give runc root privilege to start the
system container on your host.

sysvisor-runc does not support running without root privilege (i.e., rootless).
`,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  "bundle, b",
			Value: "",
			Usage: "path to the root of the bundle directory (i.e., rootfs)",
		},
	},
	Action: func(context *cli.Context) error {
		if err := checkArgs(context, 0, exactArgs); err != nil {
			return err
		}

		checkNoFile := func(name string) error {
			_, err := os.Stat(name)
			if err == nil {
				return fmt.Errorf("File %s exists. Remove it first", name)
			}
			if !os.IsNotExist(err) {
				return err
			}
			return nil
		}

		bundle := context.String("bundle")

		spec, err := syscontSpec.Example(bundle)
		if err != nil {
			return err
		}

		if err := syscontSpec.ConvertSpec(spec, false); err != nil {
			return err
		}

		if bundle != "" {
			if err := os.Chdir(bundle); err != nil {
				return err
			}
		}

		if err := checkNoFile(specConfig); err != nil {
			return err
		}

		data, err := json.MarshalIndent(spec, "", "\t")
		if err != nil {
			return err
		}
		return ioutil.WriteFile(specConfig, data, 0666)
	},
}

// loadSpec loads the specification from the provided path
// and converts it to a system container spec.
func loadSpec(cPath string) (spec *specs.Spec, err error) {
	cf, err := os.Open(cPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("JSON specification file %s not found", cPath)
		}
		return nil, err
	}
	defer cf.Close()

	if err = json.NewDecoder(cf).Decode(&spec); err != nil {
		return nil, err
	}

	err = syscontSpec.ConvertSpec(spec, false)
	if err != nil {
		return nil, fmt.Errorf("error in system container spec: %v", err)
	}

	return spec, validateProcessSpec(spec.Process)
}

func createLibContainerRlimit(rlimit specs.POSIXRlimit) (configs.Rlimit, error) {
	rl, err := strToRlimit(rlimit.Type)
	if err != nil {
		return configs.Rlimit{}, err
	}
	return configs.Rlimit{
		Type: rl,
		Hard: rlimit.Hard,
		Soft: rlimit.Soft,
	}, nil
}
