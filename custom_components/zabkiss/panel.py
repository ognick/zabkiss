from __future__ import annotations

import pathlib

from homeassistant.components.http import StaticPathConfig
from homeassistant.components.panel_custom import async_register_panel
from homeassistant.core import HomeAssistant

_FRONTEND_DIR = pathlib.Path(__file__).parent / "frontend"
_STATIC_URL = "/api/zabkiss/frontend"

# Cache-busting version — bump when frontend changes.
_VERSION = "0.2.0"


async def async_setup_panel(hass: HomeAssistant) -> None:
    await hass.http.async_register_static_paths([
        StaticPathConfig(_STATIC_URL, str(_FRONTEND_DIR), cache_headers=False)
    ])

    await async_register_panel(
        hass,
        webcomponent_name="zabkiss-panel",
        frontend_url_path="zabkiss",
        sidebar_title="ZabKiss",
        sidebar_icon="mdi:shield-check",
        module_url=f"{_STATIC_URL}/zabkiss-panel.js?v={_VERSION}",
        require_admin=True,
    )
