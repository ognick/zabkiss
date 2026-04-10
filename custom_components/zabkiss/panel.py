from __future__ import annotations

import pathlib

from homeassistant.components.panel_custom import async_register_panel
from homeassistant.core import HomeAssistant

_FRONTEND_DIR = pathlib.Path(__file__).parent / "frontend"
_STATIC_URL = "/api/zabkiss/frontend"


async def async_setup_panel(hass: HomeAssistant) -> None:
    try:
        hass.http.register_static_path(_STATIC_URL, str(_FRONTEND_DIR), cache_headers=False)
    except RuntimeError:
        pass  # already registered on entry reload

    await async_register_panel(
        hass,
        component_name="zabkiss-panel",
        frontend_url_path="zabkiss",
        sidebar_title="ZabKiss",
        sidebar_icon="mdi:shield-check",
        module_url=f"{_STATIC_URL}/zabkiss-panel.js",
        require_admin=True,
        trust_external_script=False,
    )
