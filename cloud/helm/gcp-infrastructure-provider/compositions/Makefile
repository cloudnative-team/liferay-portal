MOCK ?= mock.json

LAYERS = backup elasticsearch init k8s_resources managed_service_details overlay security sql storage

.PHONY: all clean fmt generate-models lint run test $(LAYERS)

all: fmt lint test

clean:
	rm -rf vendor/ kcl.mod.lock

fmt:
	kcl fmt *.k
	kcl fmt tests/*.k

generate-models:
	./scripts/generate_models.sh

# Type-check each layer standalone. With the multi-step pipeline, this is
# the real lint target -- there is no bundle to validate. The Helm template
# (templates/compositions.yaml + templates/_helpers.tpl) produces one
# function-kcl step per layer at install time, so each layer must compile
# on its own.
lint:
	@for layer in $(LAYERS); \
	do \
		kcl run $${layer}.k -D params='{"oxr": {"spec": {"database": {}}}, "ctx": {"apiextensions.crossplane.io/environment": {}}}' > /dev/null || exit 1; \
	done

# Render the full composite (all layers aggregated via main.k) using MOCK.
run:
	@if [ ! -f $(MOCK) ]; then echo "Please provide a mock.json file."; exit 1; fi
	@kcl run main.k -D params=$$(cat $(MOCK) | jq -r '.params | tojson')

test:
	@PARAMS=$$(cat tests/arz_dev2025_params.json | jq -c) && \
	CLUSTER_REFS=$$(cat tests/cluster_extracted_resources.json | jq -c) && \
	cd tests && kcl test . -D params="$$PARAMS" -D cluster_refs="$$CLUSTER_REFS"

# Per-layer render: `make backup` prints only that layer's YAML for MOCK.
$(LAYERS):
	@if [ ! -f $(MOCK) ]; then echo "Please provide a mock.json file."; exit 1; fi
	@kcl run $@.k -D params=$$(cat $(MOCK) | jq -r '.params | tojson')
