//go:generate go tool controller-gen crd paths=./... output:crd:artifacts:config=config/crd/bases
//go:generate go tool controller-gen object paths=./api/...
//go:generate go tool controller-gen rbac:roleName=manager-role paths=./... output:rbac:artifacts:config=config/rbac

package main