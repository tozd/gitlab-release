package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"github.com/alecthomas/kong"
	"github.com/pkg/errors"

	"gitlab.com/tozd/gitlab/release"
)

const exitCode = 2

// We use our own type=string together with defaults to render placeholder correctly.
// See: https://github.com/alecthomas/kong/issues/243
type stringMapper struct{}

func (stringMapper) Decode(ctx *kong.DecodeContext, target reflect.Value) error {
	return errors.WithStack(ctx.Scan.PopValueInto("string", target.Addr().Interface()))
}

func (stringMapper) PlaceHolder(flag *kong.Flag) string {
	if flag.PlaceHolder == "" {
		panic(`placeholder not defined with type:"string"`)
	}
	return flag.PlaceHolder
}

func main() {
	var config *release.Config
	ctx := kong.Parse(config,
		kong.Description(
			filepath.Base(os.Args[0])+" syncs tags in your git repository and a changelog in Keep a Changelog "+
				"format with releases of your GitLab project.\n\nSome flags you can provide as environment variables.",
		),
		kong.NamedMapper("string", stringMapper{}),
	)

	err := release.SyncAll(config)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "error: %+v", err)
		ctx.Exit(exitCode)
	}
}
