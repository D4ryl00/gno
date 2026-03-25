package parse

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/gnolang/gno/contribs/gnodoctor/internal/model"
)

// minimalGenesis holds only the fields gnodoctor needs from genesis.json.
// Using encoding/json avoids the amino codec requirement for app_state, which
// would otherwise require importing gno.land/pkg/gnoland and the full GnoVM.
type minimalGenesis struct {
	ChainID     string             `json:"chain_id"`
	GenesisTime time.Time          `json:"genesis_time"`
	Validators  []minimalValidator `json:"validators"`
}

type minimalValidator struct {
	Address string          `json:"address"`
	PubKey  json.RawMessage `json:"pub_key"`
	Power   json.Number     `json:"power"`
	Name    string          `json:"name"`
}

func LoadGenesis(path string) (model.Genesis, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return model.Genesis{}, fmt.Errorf("reading genesis file: %w", err)
	}

	var doc minimalGenesis
	if err := json.Unmarshal(data, &doc); err != nil {
		return model.Genesis{}, fmt.Errorf("parsing genesis file: %w", err)
	}

	out := model.Genesis{
		Path:         path,
		ChainID:      doc.ChainID,
		GenesisTime:  doc.GenesisTime,
		ValidatorNum: len(doc.Validators),
		Validators:   make([]model.Validator, 0, len(doc.Validators)),
	}

	for _, val := range doc.Validators {
		power, _ := strconv.ParseInt(val.Power.String(), 10, 64)
		out.Validators = append(out.Validators, model.Validator{
			Name:    val.Name,
			Address: val.Address,
			PubKey:  string(val.PubKey),
			Power:   power,
		})
	}

	return out, nil
}
