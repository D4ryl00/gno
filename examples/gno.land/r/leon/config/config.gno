package config

import (
	"errors"
	"std"
	"strings"
	"time"

	"gno.land/p/demo/avl"
	"gno.land/p/demo/ownable"
	"gno.land/p/demo/seqid"
)

var (
	cfgID   seqid.ID
	configs = avl.NewTree()

	absPath = strings.TrimPrefix(std.CurrentRealm().PkgPath(), std.ChainDomain())

	// SafeObjects
	OwnableMain   = ownable.NewWithAddress("g125em6arxsnj49vx35f0n0z34putv5ty3376fg5")
	OwnableBackup = ownable.NewWithAddress("g1lavlav7zwsjqlzzl3qdl3nl242qtf638vnhdjh")

	ErrUnauthorized = errors.New("leon's config: unauthorized")
)

type Config struct {
	id      seqid.ID
	name    string
	lines   string
	updated time.Time
}

func AddConfig(cur realm, name, lines string) {
	if !IsAuthorized(std.PreviousRealm().Address()) {
		panic(ErrUnauthorized)
	}

	id := cfgID.Next()
	configs.Set(id.String(), Config{
		id:      id,
		name:    name,
		lines:   lines,
		updated: time.Now(),
	})
}

func EditConfig(cur realm, id string, name, lines string) {
	if !IsAuthorized(std.PreviousRealm().Address()) {
		panic(ErrUnauthorized)
	}

	raw, ok := configs.Remove(id)
	if !ok {
		panic("no config with that id")
	}

	conf := raw.(Config)
	// Overwrites data
	conf.lines = lines
	conf.name = name
	conf.updated = time.Now()
}

func RemoveConfig(cur realm, id string) {
	if !IsAuthorized(std.PreviousRealm().Address()) {
		panic(ErrUnauthorized)
	}

	if _, ok := configs.Remove(id); !ok {
		panic("no config with that id")
	}
}

func UpdateBanner(cur realm, newBanner string) {
	if !IsAuthorized(std.PreviousRealm().Address()) {
		panic(ErrUnauthorized)
	}

	banner = newBanner
}

func IsAuthorized(addr std.Address) bool {
	return addr == OwnableMain.Owner() || addr == OwnableBackup.Owner()
}
