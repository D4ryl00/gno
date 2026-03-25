package parse

import (
	"testing"

	"github.com/gnolang/gno/contribs/gnodoctor/internal/model"
	"github.com/stretchr/testify/require"
)

func TestParseJSONLine(t *testing.T) {
	source := model.Source{Path: "/tmp/validator.log", Node: "validator_1", Role: model.RoleValidator}
	line := `{"level":"info","ts":1774017464.5705216,"msg":"Finalizing commit of block","height":12}`

	event, warning := ParseLogLine(source, line, 1)

	require.Empty(t, warning)
	require.Equal(t, "json", event.Format)
	require.Equal(t, model.EventFinalizeCommit, event.Kind)
	require.True(t, event.HasTimestamp)
	require.EqualValues(t, 12, event.Height)
}

func TestParseConsoleLineWithANSIAndContainerPrefix(t *testing.T) {
	source := model.Source{Path: "/tmp/sentry.log", Node: "sentry_a", Role: model.RoleSentry}
	line := "gnoland-1  | 2026-03-20T14:37:08.485Z\t\x1b[34mINFO \x1b[0m\tAdded peer\t{\"module\":\"p2p\",\"peer\":\"Peer{abc}\"}"

	event, warning := ParseLogLine(source, line, 7)

	require.Empty(t, warning)
	require.Equal(t, "console", event.Format)
	require.Equal(t, model.EventAddedPeer, event.Kind)
	require.Equal(t, "info", event.Level)
	require.Equal(t, "Added peer", event.Message)
}
