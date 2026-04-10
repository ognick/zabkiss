from homeassistant.core import HomeAssistant
from homeassistant.helpers.storage import Store

_STORE_KEY = "zabkiss.policy"
_STORE_VERSION = 1


class ZabKissStorage:
    def __init__(self, hass: HomeAssistant) -> None:
        self._store: Store = Store(hass, _STORE_VERSION, _STORE_KEY)
        self._entities: list[str] = []

    async def async_load(self) -> None:
        data = await self._store.async_load()
        self._entities = (data or {}).get("entities", [])

    def get_entities(self) -> list[str]:
        return self._entities

    async def async_save_entities(self, entities: list[str]) -> None:
        self._entities = entities
        await self._store.async_save({"entities": entities})
