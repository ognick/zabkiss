from __future__ import annotations

from homeassistant.config_entries import ConfigEntry
from homeassistant.core import HomeAssistant

from .api import ZabKissPolicyView, DATA_STORAGE
from .panel import async_setup_panel
from .storage import ZabKissStorage

DOMAIN = "zabkiss"
_DATA_HTTP_REGISTERED = "http_registered"


async def async_setup(hass: HomeAssistant, config: dict) -> bool:
    hass.data.setdefault(DOMAIN, {})
    return True


async def async_setup_entry(hass: HomeAssistant, entry: ConfigEntry) -> bool:
    hass.data.setdefault(DOMAIN, {})

    storage = ZabKissStorage(hass)
    await storage.async_load()
    hass.data[DOMAIN][DATA_STORAGE] = storage

    if not hass.data[DOMAIN].get(_DATA_HTTP_REGISTERED):
        hass.http.register_view(ZabKissPolicyView())
        hass.data[DOMAIN][_DATA_HTTP_REGISTERED] = True

    await async_setup_panel(hass)
    return True


async def async_unload_entry(hass: HomeAssistant, entry: ConfigEntry) -> bool:
    hass.data[DOMAIN].pop(DATA_STORAGE, None)
    return True
