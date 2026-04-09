IMAGE             ?= gnoland:local
GNOKEY_IMAGE      ?= gnokey:local
GNOGENESIS_IMAGE  ?= gnogenesis:local
GNO_ROOT          ?= $(abspath $(dir $(lastword $(MAKEFILE_LIST)))/../..)
WORK_ROOT         ?= /tmp/gno-val-tests
NODE              ?=

PROJECT_01 = scenario-01-five-validators-reset-four
PROJECT_02 = scenario-02-four-validators-restart-staggered
PROJECT_03 = scenario-03-four-validators-restart-parallel
PROJECT_04 = scenario-04-counter-realm-churn
PROJECT_05 = scenario-05-sentry-ip-rotation
PROJECT_06 = scenario-06-gas-nondeterminism-check
PROJECT_07 = scenario-07-five-validators-reset-one
PROJECT_08 = scenario-08-five-validators-reset-two-below-consensus
PROJECT_09 = scenario-09-five-validators-safe-reset-one
PROJECT_10 = scenario-10-five-validators-safe-reset-two-below-consensus

.PHONY: build-gnoland-image build-gnokey-image build-gnogenesis-image build-images clone-gno \
        scenario-01 scenario-02 scenario-03 scenario-04 scenario-05 scenario-06 scenario-07 scenario-08 scenario-09 scenario-10 \
        logs-01 logs-02 logs-03 logs-04 logs-05 logs-06 logs-07 logs-08 logs-09 logs-10 \
        clean clean-01 clean-02 clean-03 clean-04 clean-05 clean-06 clean-07 clean-08 clean-09 clean-10 \
        status

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

clean-01:
	rm -rf $(WORK_ROOT)/$(PROJECT_01)

clean-02:
	rm -rf $(WORK_ROOT)/$(PROJECT_02)

clean-03:
	rm -rf $(WORK_ROOT)/$(PROJECT_03)

clean-04:
	rm -rf $(WORK_ROOT)/$(PROJECT_04)

clean-05:
	rm -rf $(WORK_ROOT)/$(PROJECT_05)

clean-06:
	rm -rf $(WORK_ROOT)/$(PROJECT_06)

clean-07:
	rm -rf $(WORK_ROOT)/$(PROJECT_07)

clean-08:
	rm -rf $(WORK_ROOT)/$(PROJECT_08)

clean-09:
	rm -rf $(WORK_ROOT)/$(PROJECT_09)

clean-10:
	rm -rf $(WORK_ROOT)/$(PROJECT_10)

scenario-01:
	IMAGE_NAME=$(IMAGE) GNOKEY_IMAGE=$(GNOKEY_IMAGE) GNOGENESIS_IMAGE=$(GNOGENESIS_IMAGE) GNO_ROOT=$(GNO_ROOT) WORK_ROOT=$(WORK_ROOT) ./scenarios/01_five_validators_reset_four.sh

scenario-02:
	IMAGE_NAME=$(IMAGE) GNOKEY_IMAGE=$(GNOKEY_IMAGE) GNOGENESIS_IMAGE=$(GNOGENESIS_IMAGE) GNO_ROOT=$(GNO_ROOT) WORK_ROOT=$(WORK_ROOT) ./scenarios/02_four_validators_restart_staggered.sh

scenario-03:
	IMAGE_NAME=$(IMAGE) GNOKEY_IMAGE=$(GNOKEY_IMAGE) GNOGENESIS_IMAGE=$(GNOGENESIS_IMAGE) GNO_ROOT=$(GNO_ROOT) WORK_ROOT=$(WORK_ROOT) ./scenarios/03_four_validators_restart_parallel.sh

scenario-04:
	IMAGE_NAME=$(IMAGE) GNOKEY_IMAGE=$(GNOKEY_IMAGE) GNOGENESIS_IMAGE=$(GNOGENESIS_IMAGE) GNO_ROOT=$(GNO_ROOT) WORK_ROOT=$(WORK_ROOT) ./scenarios/04_counter_realm_churn.sh

scenario-05:
	IMAGE_NAME=$(IMAGE) GNOKEY_IMAGE=$(GNOKEY_IMAGE) GNOGENESIS_IMAGE=$(GNOGENESIS_IMAGE) GNO_ROOT=$(GNO_ROOT) WORK_ROOT=$(WORK_ROOT) ./scenarios/05_sentry_ip_rotation.sh

scenario-06:
	IMAGE_NAME=$(IMAGE) GNOKEY_IMAGE=$(GNOKEY_IMAGE) GNOGENESIS_IMAGE=$(GNOGENESIS_IMAGE) GNO_ROOT=$(GNO_ROOT) WORK_ROOT=$(WORK_ROOT) ./scenarios/06_gas_nondeterminism_check.sh

scenario-07:
	IMAGE_NAME=$(IMAGE) GNOKEY_IMAGE=$(GNOKEY_IMAGE) GNOGENESIS_IMAGE=$(GNOGENESIS_IMAGE) GNO_ROOT=$(GNO_ROOT) WORK_ROOT=$(WORK_ROOT) ./scenarios/07_five_validators_reset_one.sh

scenario-08:
	IMAGE_NAME=$(IMAGE) GNOKEY_IMAGE=$(GNOKEY_IMAGE) GNOGENESIS_IMAGE=$(GNOGENESIS_IMAGE) GNO_ROOT=$(GNO_ROOT) WORK_ROOT=$(WORK_ROOT) ./scenarios/08_five_validators_reset_two_below_consensus.sh

scenario-09:
	IMAGE_NAME=$(IMAGE) GNOKEY_IMAGE=$(GNOKEY_IMAGE) GNOGENESIS_IMAGE=$(GNOGENESIS_IMAGE) GNO_ROOT=$(GNO_ROOT) WORK_ROOT=$(WORK_ROOT) ./scenarios/09_five_validators_safe_reset_one.sh

scenario-10:
	IMAGE_NAME=$(IMAGE) GNOKEY_IMAGE=$(GNOKEY_IMAGE) GNOGENESIS_IMAGE=$(GNOGENESIS_IMAGE) GNO_ROOT=$(GNO_ROOT) WORK_ROOT=$(WORK_ROOT) ./scenarios/10_five_validators_safe_reset_two_below_consensus.sh

logs-01:
	docker compose -p $(PROJECT_01) logs -f $(NODE)

logs-02:
	docker compose -p $(PROJECT_02) logs -f $(NODE)

logs-03:
	docker compose -p $(PROJECT_03) logs -f $(NODE)

logs-04:
	docker compose -p $(PROJECT_04) logs -f $(NODE)

logs-05:
	docker compose -p $(PROJECT_05) logs -f $(NODE)

logs-06:
	docker compose -p $(PROJECT_06) logs -f $(NODE)

logs-07:
	docker compose -p $(PROJECT_07) logs -f $(NODE)

logs-08:
	docker compose -p $(PROJECT_08) logs -f $(NODE)

logs-09:
	docker compose -p $(PROJECT_09) logs -f $(NODE)

logs-10:
	docker compose -p $(PROJECT_10) logs -f $(NODE)

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
