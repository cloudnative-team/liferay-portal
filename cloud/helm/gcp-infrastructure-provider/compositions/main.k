import .backup
import .elasticsearch
import .init
import .k8s_resources
import .managed_service_details
import .overlay
import .security
import .sql
import .storage

items = backup.items + elasticsearch.items + init.items + k8s_resources.items + managed_service_details.items + overlay.items + security.items + sql.items + storage.items
