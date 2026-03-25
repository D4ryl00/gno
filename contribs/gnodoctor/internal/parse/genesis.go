package parse

import (
	"github.com/gnolang/gno/contribs/gnodoctor/internal/model"
	bft "github.com/gnolang/gno/tm2/pkg/bft/types"
	"github.com/gnolang/gno/tm2/pkg/crypto"
)

func LoadGenesis(path string) (model.Genesis, error) {
	doc, err := bft.GenesisDocFromFile(path)
	if err != nil {
		return model.Genesis{}, err
	}

	out := model.Genesis{
		Path:         path,
		ChainID:      doc.ChainID,
		GenesisTime:  doc.GenesisTime,
		ValidatorNum: len(doc.Validators),
		Validators:   make([]model.Validator, 0, len(doc.Validators)),
	}

	for _, val := range doc.Validators {
		out.Validators = append(out.Validators, model.Validator{
			Name:    val.Name,
			Address: val.Address.Bech32().String(),
			PubKey:  crypto.PubKeyToBech32(val.PubKey),
			Power:   val.Power,
		})
	}

	return out, nil
}
