import context_variables as cv

get_items = lambda obs: any, vars: {str:any} -> [any] {
    _activeDataPlane = str(vars?.activeDataPlane or "blue")
    _meta_name = str(vars?.baseName or "")

    _res = obs?.resources or {}
    _grant_conditions = _res?["{}-db-grant-{}".format(_meta_name, _activeDataPlane)]?.resource?.status?.conditions or []
    _is_ready = any c in _grant_conditions {(c.type == "Ready" or c.type == "Complete") and c.status == "True"}
    [
        {
            apiVersion = "gcp.liferay.com/v1alpha1"
            kind = "LiferayInfrastructure"
            status = {managedServiceDetailsReady = _is_ready}
        }
    ]
}

_vars = {
    "activeDataPlane": cv.activeDataPlane
    "baseName": cv.baseName
}

items = {"items": get_items(cv.obs, _vars)}
