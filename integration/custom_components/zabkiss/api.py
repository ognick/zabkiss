from __future__ import annotations

from http import HTTPStatus
from typing import TYPE_CHECKING

from homeassistant.components.http import HomeAssistantView

if TYPE_CHECKING:
    from aiohttp.web import Request, Response

DOMAIN = "zabkiss"
DATA_STORAGE = "storage"


class ZabKissPolicyView(HomeAssistantView):
    url = "/api/zabkiss/policy"
    name = "api:zabkiss:policy"
    requires_auth = True

    async def get(self, request: Request) -> Response:
        hass = request.app["hass"]
        storage = hass.data.get(DOMAIN, {}).get(DATA_STORAGE)
        if storage is None:
            return self.json_message("not initialized", HTTPStatus.SERVICE_UNAVAILABLE)
        return self.json({"entities": storage.get_entities()})

    async def post(self, request: Request) -> Response:
        hass = request.app["hass"]
        storage = hass.data.get(DOMAIN, {}).get(DATA_STORAGE)
        if storage is None:
            return self.json_message("not initialized", HTTPStatus.SERVICE_UNAVAILABLE)

        try:
            body = await request.json()
        except Exception:
            return self.json_message("invalid json", HTTPStatus.BAD_REQUEST)

        if not isinstance(body, dict) or not isinstance(body.get("entities"), list):
            return self.json_message("entities must be a list", HTTPStatus.BAD_REQUEST)

        entities = [e for e in body["entities"] if isinstance(e, str)]
        await storage.async_save_entities(entities)
        return self.json({"ok": True})
