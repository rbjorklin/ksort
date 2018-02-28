/*
Copyright (c) 2019 Kazuki Suda <kazuki.suda@gmail.com>

For the full copyright and license information, please view the LICENSE
file that was distributed with this source code.
*/

package cmd

import (
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/golang/glog"
	"github.com/spf13/cobra"
	"github.com/superbrothers/ksort/version"
	util "k8s.io/helm/pkg/releaseutil"
	"k8s.io/helm/pkg/tiller"
)

var (
	printVersion = false
)

const (
	ksortLong = `When installing manifests, they should be sorted in a proper order by Kind.
For example, Namespace object must be in the first place when installing them.

ksort sorts manfest files in a proper order by Kind, which is implementd by
using SortByKind function in Kubernetes Helm.`

	ksortExample = `# Sort manifest files in the deploy directory, and output the result to the stdout.
ksort ./deploy

# To pass the result into the stdin of kubectl apply command is also convenient.
ksort ./deploy | kubectl apply -f -`

	kindUnknown = "Unknown"
)

type options struct {
	filename string
}

func init() {
	flag.Set("logtostderr", "true")
}

func New() *cobra.Command {
	o := options{}

	cmd := &cobra.Command{
		Use:     "ksort FILENAME",
		Short:   "ksort sorts manfest files in a proper order by Kind.",
		Long:    ksortLong,
		Example: ksortExample,
		RunE: func(cmd *cobra.Command, args []string) error {
			if printVersion {
				fmt.Printf("%#v\n", version.NewInfo())
				return nil
			}

			if err := o.complete(cmd, args); err != nil {
				return err
			}

			cmd.SilenceUsage = true

			return o.run()
		},
	}

	cmd.Flags().BoolVar(&printVersion, "version", printVersion, "Print the version and exit")
	cmd.Flags().AddGoFlagSet(flag.CommandLine)

	// Workaround for this issue:
	// https://github.com/kubernetes/kubernetes/issues/17162
	flag.CommandLine.Parse([]string{})

	return cmd
}

func (o *options) complete(cmd *cobra.Command, args []string) error {
	switch len(args) {
	case 0:
		return errors.New("filename is required")
	case 1:
		// verify manifest file exists
		if _, err := os.Stat(args[0]); err != nil {
			return err
		}

		o.filename = args[0]
	default:
		return errors.New("only one of filename is allowed")
	}

	return nil
}

func (o *options) run() error {
	contents := map[string]string{}

	glog.V(2).Infof("Walking the file tree rooted at %q", o.filename)

	err := filepath.Walk(o.filename, func(path string, info os.FileInfo, err error) error {
		glog.V(2).Infof("Visiting %q", path)

		if err != nil {
			return fmt.Errorf("Failed to access a path %q: %v\n", o.filename, err)
		}

		if info.IsDir() {
			glog.V(2).Infof("Skip %q because it's directory", path)
			return nil
		}

		content, err := ioutil.ReadFile(path)
		if err != nil {
			return fmt.Errorf("Failed to read a file %q: %v\n", path, err)
		}

		contents[path] = string(content)

		return nil
	})

	if err != nil {
		return err
	}

	if len(contents) == 0 {
		return fmt.Errorf("File does not exist in %s", o.filename)
	}

	// extract kind and name
	re := regexp.MustCompile("kind:(.*)\n")
	// YAML separator
	sep := regexp.MustCompile("(?m)^---.*$")
	manifests := []tiller.Manifest{}
	for k, v := range contents {
		docs := sep.Split(v, -1)
		for _, doc := range docs {
			if len(doc) == 0 {
				continue
			}
			match := re.FindStringSubmatch(doc)
			h := kindUnknown
			if len(match) == 2 {
				h = strings.TrimSpace(match[1])
			}
			doc = strings.Trim(doc, "\n")
			m := tiller.Manifest{Name: k, Content: doc, Head: &util.SimpleHead{Kind: h}}
			manifests = append(manifests, m)
			glog.V(2).Infof("Found %s in %q", h, k)
		}
	}
	glog.V(2).Infof("Found %d objects in total", len(manifests))

	for _, m := range tiller.SortByKind(manifests) {
		fmt.Printf("---\n# Source: %s\n", m.Name)

		if m.Head.Kind == kindUnknown {
			fmt.Println("# WARNING: It looks like that this file is not a manifest file")
			continue
		}

		fmt.Println(m.Content)
	}

	return nil
}
