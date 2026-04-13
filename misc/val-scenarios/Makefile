WORK_ROOT         ?= /tmp/gno-val-tests
NODE              ?=

# Remote fork support: set GH_USER to build images from a GitHub fork.
# GH_REPO defaults to "gno" and GH_BRANCH defaults to "master".
# e.g. make build-images GH_USER=gnolang
#      make build-images GH_USER=gnolang GH_REPO=gno GH_BRANCH=feat/my-branch
# Image tags are derived automatically: gnoland:<GH_USER>-<GH_BRANCH>
# (slashes in the branch name are replaced with dashes).
GH_USER           ?=
GH_REPO           ?= gno
GH_BRANCH         ?= master

_REMOTE_GNO_ROOT  := /tmp/gno-remote-build
_FETCH_STAMP      := $(_REMOTE_GNO_ROOT)/.fetched

ifneq ($(GH_USER),)
_IMAGE_TAG        := $(GH_USER)-$(subst /,-,$(GH_BRANCH))
IMAGE             ?= gnoland:$(_IMAGE_TAG)
GNOKEY_IMAGE      ?= gnokey:$(_IMAGE_TAG)
GNOGENESIS_IMAGE  ?= gnogenesis:$(_IMAGE_TAG)
GNO_ROOT          := $(_REMOTE_GNO_ROOT)
_FETCH_PREREQ     := $(_FETCH_STAMP)
else
IMAGE             ?= gnoland:local
GNOKEY_IMAGE      ?= gnokey:local
GNOGENESIS_IMAGE  ?= gnogenesis:local
GNO_ROOT          ?= $(abspath $(dir $(lastword $(MAKEFILE_LIST)))/../..)
_FETCH_PREREQ     :=
endif

# Auto-discover all scenario scripts ordered by filename.
SCENARIO_SCRIPTS := $(sort $(wildcard scenarios/*.sh))

# Extract the numeric prefix from a script path.
# e.g. scenarios/01_five_validators_reset_four.sh -> 01
scenario_num = $(word 1,$(subst _, ,$(notdir $(patsubst %.sh,%,$(1)))))

# Generate scenario-NN / logs-NN / clean-NN rules for every discovered script.
# The docker-compose project name matches the scenario_init argument: scenario-NN.
define SCENARIO_RULES
.PHONY: scenario-$(call scenario_num,$(1)) logs-$(call scenario_num,$(1)) clean-$(call scenario_num,$(1))

scenario-$(call scenario_num,$(1)):
	IMAGE_NAME=$(IMAGE) GNOKEY_IMAGE=$(GNOKEY_IMAGE) GNOGENESIS_IMAGE=$(GNOGENESIS_IMAGE) GNO_ROOT=$(GNO_ROOT) WORK_ROOT=$(WORK_ROOT) ./$(1)

logs-$(call scenario_num,$(1)):
	docker compose -p scenario-$(call scenario_num,$(1)) logs -f $(NODE)

clean-$(call scenario_num,$(1)):
	rm -rf $(WORK_ROOT)/scenario-$(call scenario_num,$(1))

endef

$(foreach s,$(SCENARIO_SCRIPTS),$(eval $(call SCENARIO_RULES,$(s))))

.PHONY: build-gnoland-image build-gnokey-image build-gnogenesis-image build-images fetch-remote clone-gno clean status

# Stamp-file rule: clones once; subsequent builds skip it until fetch-remote is called.
$(_FETCH_STAMP):
	rm -rf $(_REMOTE_GNO_ROOT)
	git clone --depth 1 --branch $(GH_BRANCH) \
		https://github.com/$(GH_USER)/$(GH_REPO).git \
		$(_REMOTE_GNO_ROOT)
	touch $@

# Force a fresh clone regardless of the stamp file.
fetch-remote:
	rm -f $(_FETCH_STAMP)
	$(MAKE) $(_FETCH_STAMP) GH_USER=$(GH_USER) GH_REPO=$(GH_REPO) GH_BRANCH=$(GH_BRANCH)

clone-gno:
	@if [ -d "$(GNO_ROOT)" ]; then \
		echo "$(GNO_ROOT) already exists, skipping clone"; \
	else \
		git clone https://github.com/gnolang/gno.git $(GNO_ROOT); \
	fi

build-gnoland-image: $(_FETCH_PREREQ)
	docker build -t $(IMAGE) -f $(GNO_ROOT)/Dockerfile --target gnoland $(GNO_ROOT)

build-gnokey-image: $(_FETCH_PREREQ)
	docker build -t $(GNOKEY_IMAGE) -f $(GNO_ROOT)/Dockerfile --target gnokey $(GNO_ROOT)

build-gnogenesis-image: $(_FETCH_PREREQ)
	docker build -t $(GNOGENESIS_IMAGE) -f $(GNO_ROOT)/Dockerfile --target gnocontribs $(GNO_ROOT)

build-images: build-gnoland-image build-gnokey-image build-gnogenesis-image

clean:
	rm -rf $(WORK_ROOT)

status:
	@while true; do \
		printf '\033[2J\033[H'; \
		date '+%H:%M:%S'; \
		echo; \
		docker ps --format '{{.Names}}\t{{.Ports}}' | grep -- '->26657/tcp' | sort | \
		while IFS=$$'\t' read -r name ports; do \
			port=$$(printf '%s' "$$ports" | grep -oE '0\.0\.0\.0:[0-9]+' | cut -d: -f2); \
			height=$$(curl -fsS "http://127.0.0.1:$$port/status" 2>/dev/null \
				| jq -r '.result.sync_info.latest_block_height // "offline"' 2>/dev/null \
				|| echo offline); \
			printf '%-40s  height=%s\n' "$$name" "$$height"; \
		done; \
		sleep 2; \
	done
