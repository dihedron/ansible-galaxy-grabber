package main

import (
	"fmt"
	"log/slog"
	"os"
	"path"

	"github.com/Masterminds/semver"
	"github.com/cavaliergopher/grab/v3"
	"github.com/dihedron/ansible-galaxy-grabber/helpers"
	"github.com/dihedron/rawdata"
	"github.com/fatih/color"
	"github.com/go-resty/resty/v2"
	"github.com/mattn/go-isatty"
)

type Collections struct {
	Entries []Collection `json:"collections" yaml:"collections"`
}

func (c *Collections) UnmarshalFlag(value string) error {
	tmp := Collections{}
	*c = tmp
	return rawdata.UnmarshalInto(value, &c.Entries)
}

type Collection struct {
	Namespace   string  `json:"namespace" yaml:"namespace"`
	Collection  string  `json:"collection" yaml:"collection"`
	Constraints *string `json:"constraint,omitempty" yaml:"constraint,omitempty"`
}

func (c *Collection) Download(client *resty.Client, destination string) error {

	directory := path.Join(destination, c.Namespace, c.Collection)

	if err := os.MkdirAll(directory, os.ModePerm); err != nil {
		slog.Error("error creating directory", "namespace", c.Namespace, "collection", c.Collection, "error", err)
		return err
	}

	result := &CollectionMetadata{}
	_, err := client.
		R().
		SetQueryParam("namespace", c.Namespace).
		SetQueryParam("name", c.Collection).
		EnableTrace().
		SetResult(result).
		Get("https://galaxy.ansible.com/api/internal/ui/repo-or-collection-detail/")

	if err != nil {
		slog.Error("error downloading collection index", "namespace", c.Namespace, "collection", c.Collection, "error", err)
		return err
	}

	f, err := os.Create(path.Join(directory, "index.json"))
	if err != nil {
		slog.Error("error opening collection index.json for output", "namespace", c.Namespace, "collection", c.Collection, "error", err)
		return err
	}
	defer f.Close()

	if _, err = f.WriteString(helpers.ToPrettyJSON(result)); err != nil {
		slog.Error("error persisting index.json", "namespace", c.Namespace, "collection", c.Collection, "error", err)
		return err
	}

	var filter *semver.Constraints
	if c.Constraints != nil {
		filter, err = semver.NewConstraint(*c.Constraints)
		if err != nil {
			slog.Error("error parsing constraints", "namespace", c.Namespace, "collection", c.Collection, "constraints", *c.Constraints, "error", err)
			return err
		}
	}

	if isatty.IsTerminal(os.Stdout.Fd()) {
		fmt.Printf("collection %s - %s (output: %s):\n", color.HiMagentaString(c.Namespace), color.HiMagentaString(c.Collection), directory)
	} else {
		fmt.Printf("collection %s - %s (output: %s):\n", c.Namespace, c.Collection, directory)
	}

	for _, version := range result.Data.Collection.AllVersions {
		link := fmt.Sprintf("https://galaxy.ansible.com%s", version.DownloadURL)
		if filter != nil {
			v, err := semver.NewVersion(version.Version)
			if err != nil {
				slog.Error("error parsing version", "namespace", c.Namespace, "collection", c.Collection, "version", version.Version, "error", err)
				return err
			}
			if !filter.Check(v) {
				if isatty.IsTerminal(os.Stdout.Fd()) {
					fmt.Printf(" - v%s: %s [URL: %s]\n", version.Version, color.YellowString("skipped"), link)
				} else {
					fmt.Printf(" - v%s: skipped [URL: %s]\n", version.Version, link)
				}
				continue
			}
		}

		response, err := grab.Get(directory, link)
		if err != nil {
			slog.Error("error downloading collection", "namespace", c.Namespace, "collection", c.Collection, "error", err)
			if isatty.IsTerminal(os.Stdout.Fd()) {
				fmt.Printf(" - v%s: %s [URL: %s, error: %v]\n", version.Version, color.RedString("failed"), link, err)
			} else {
				fmt.Printf(" - v%s: failed [URL: %s, bytes: %d, duration: %s]\n", version.Version, link, response.Size(), response.Duration())
			}
			continue
		}
		if isatty.IsTerminal(os.Stdout.Fd()) {
			fmt.Printf(" - v%s: %s [URL: %s, bytes: %d, duration: %s]\n", version.Version, color.GreenString("success"), link, response.Size(), response.Duration())
		} else {
			fmt.Printf(" - v%s: success [URL: %s, bytes: %d, duration: %s]\n", version.Version, link, response.Size(), response.Duration())
		}
	}

	return nil
}
