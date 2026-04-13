IMAGE             ?= gnoland:local
GNOKEY_IMAGE      ?= gnokey:local
GNOGENESIS_IMAGE  ?= gnogenesis:local
GNO_ROOT          ?= $(abspath $(dir $(lastword $(MAKEFILE_LIST)))/../..)
WORK_ROOT         ?= /tmp/gno-val-tests
NODE              ?=

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

.PHONY: build-gnoland-image build-gnokey-image build-gnogenesis-image build-images clone-gno clean status

clone-gno:
	@if [ -d "$(GNO_ROOT)" ]; then \
		echo "$(GNO_ROOT) already exists, skipping clone"; \
	else \
		git clone https://github.com/gnolang/gno.git $(GNO_ROOT); \
	fi

build-gnoland-image:
	docker build -t $(IMAGE) -f $(GNO_ROOT)/Dockerfile --target gnoland $(GNO_ROOT)

build-gnokey-image:
	docker build -t $(GNOKEY_IMAGE) -f $(GNO_ROOT)/Dockerfile --target gnokey $(GNO_ROOT)

build-gnogenesis-image:
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
