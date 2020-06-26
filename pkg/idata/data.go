package idata

import (
	"github.com/nektro/andesite/pkg/itypes"

	"github.com/mitchellh/go-homedir"
)

const (
	RequiredConfigVersion = 2
	DiscordAPI            = "https://discordapp.com/api/v6"
)

var (
	Config         = new(itypes.Config)
	HomedirPath, _ = homedir.Dir()
	DataPathsPub   = map[string]string{}
	DataPathsPrv   = map[string]string{}
)
