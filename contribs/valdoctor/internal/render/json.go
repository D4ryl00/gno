package render

import (
	"encoding/json"

	"github.com/gnolang/gno/contribs/valdoctor/internal/model"
)

func JSON(report model.Report) ([]byte, error) {
	return json.MarshalIndent(report, "", "  ")
}
