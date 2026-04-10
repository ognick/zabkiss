const ACTIONABLE_DOMAINS = new Set([
  'binary_sensor', 'button', 'climate', 'cover', 'fan', 'humidifier',
  'input_boolean', 'input_button', 'input_number', 'input_select',
  'light', 'lock', 'media_player', 'number', 'remote',
  'scene', 'script', 'select', 'sensor', 'siren', 'switch', 'vacuum',
  'water_heater',
]);

const STYLES = `
  :host {
    display: block;
    padding: 16px;
    font-family: var(--paper-font-body1_-_font-family, Roboto, sans-serif);
    color: var(--primary-text-color);
    background: var(--lovelace-background, var(--primary-background-color));
    min-height: 100%;
    box-sizing: border-box;
  }
  h1 {
    margin: 0 0 16px;
    font-size: 1.4em;
    font-weight: 400;
  }
  .card {
    background: var(--card-background-color, #fff);
    border-radius: 12px;
    margin-bottom: 12px;
    box-shadow: var(--ha-card-box-shadow, 0 2px 6px rgba(0,0,0,.12));
    overflow: hidden;
  }
  .card-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 12px 16px;
    border-bottom: 1px solid var(--divider-color, #e0e0e0);
    gap: 8px;
  }
  .card-title {
    font-weight: 500;
    font-size: 1em;
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .btn-group { display: flex; gap: 6px; flex-shrink: 0; }
  .btn {
    font-size: 0.75em;
    padding: 3px 10px;
    border: 1px solid var(--primary-color, #03a9f4);
    border-radius: 12px;
    background: transparent;
    color: var(--primary-color, #03a9f4);
    cursor: pointer;
    white-space: nowrap;
    font-family: inherit;
  }
  .btn:hover {
    background: var(--primary-color, #03a9f4);
    color: var(--text-primary-color, #fff);
  }
  .entity-list { padding: 4px 0; }
  .entity-row {
    display: flex;
    align-items: center;
    padding: 7px 16px;
    gap: 10px;
    cursor: pointer;
  }
  .entity-row:hover { background: var(--secondary-background-color, rgba(0,0,0,.04)); }
  .entity-row input[type=checkbox] {
    width: 18px;
    height: 18px;
    flex-shrink: 0;
    cursor: pointer;
    accent-color: var(--primary-color, #03a9f4);
  }
  .entity-row label { cursor: pointer; flex: 1; min-width: 0; }
  .entity-name { font-size: 0.9em; }
  .entity-id {
    font-size: 0.75em;
    color: var(--secondary-text-color, #888);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .save-bar {
    position: sticky;
    bottom: 0;
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 12px 4px 4px;
    background: var(--lovelace-background, var(--primary-background-color));
  }
  .save-btn {
    background: var(--primary-color, #03a9f4);
    color: var(--text-primary-color, #fff);
    border: none;
    border-radius: 6px;
    padding: 10px 28px;
    font-size: 1em;
    cursor: pointer;
    font-family: inherit;
  }
  .save-btn:disabled { opacity: 0.5; cursor: default; }
  .status { font-size: 0.9em; color: var(--secondary-text-color, #888); }
  .empty { padding: 24px; color: var(--secondary-text-color, #888); }
`;

class ZabKissPanel extends HTMLElement {
  constructor() {
    super();
    this.attachShadow({ mode: 'open' });
    this._hass = null;
    this._enabled = new Set();
    this._initialized = false;
  }

  set hass(hass) {
    this._hass = hass;
    if (!this._initialized) {
      this._initialized = true;
      this._init();
    }
  }

  async _init() {
    this.shadowRoot.innerHTML = `<style>${STYLES}</style><div class="empty">Загрузка...</div>`;
    try {
      const policy = await this._hass.callApi('GET', 'zabkiss/policy');
      this._enabled = new Set(policy.entities ?? []);
    } catch (_) {
      this._enabled = new Set();
    }
    this._render();
    this._attachListeners();
  }

  _buildGroups() {
    const byDevice = {};
    const byDomain = {};

    for (const [entityId, state] of Object.entries(this._hass.states)) {
      const domain = entityId.split('.')[0];
      if (!ACTIONABLE_DOMAINS.has(domain)) continue;

      const regEntry = this._hass.entities?.[entityId];
      const deviceId = regEntry?.device_id;
      const device = deviceId ? this._hass.devices?.[deviceId] : null;
      const friendlyName = state.attributes?.friendly_name || entityId;

      if (device) {
        if (!byDevice[deviceId]) {
          byDevice[deviceId] = {
            label: device.name_by_user || device.name || deviceId,
            entities: [],
          };
        }
        byDevice[deviceId].entities.push({ entityId, name: friendlyName });
      } else {
        if (!byDomain[domain]) {
          byDomain[domain] = { label: domain, entities: [] };
        }
        byDomain[domain].entities.push({ entityId, name: friendlyName });
      }
    }

    return [
      ...Object.values(byDevice),
      ...Object.values(byDomain),
    ].sort((a, b) => a.label.localeCompare(b.label, 'ru'));
  }

  _esc(s) {
    return String(s)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }

  _render() {
    const groups = this._buildGroups();

    if (groups.length === 0) {
      this.shadowRoot.innerHTML = `<style>${STYLES}</style><div class="empty">Управляемые устройства не найдены</div>`;
      return;
    }

    const cards = groups.map(g => `
      <div class="card">
        <div class="card-header">
          <span class="card-title">${this._esc(g.label)}</span>
          <div class="btn-group">
            <button class="btn" data-action="all">Выбрать все</button>
            <button class="btn" data-action="none">Снять все</button>
          </div>
        </div>
        <div class="entity-list">
          ${g.entities.map(e => `
            <div class="entity-row">
              <input type="checkbox" id="cb-${this._esc(e.entityId)}"
                     data-entity="${this._esc(e.entityId)}"
                     ${this._enabled.has(e.entityId) ? 'checked' : ''}>
              <label for="cb-${this._esc(e.entityId)}">
                <div class="entity-name">${this._esc(e.name)}</div>
                <div class="entity-id">${this._esc(e.entityId)}</div>
              </label>
            </div>
          `).join('')}
        </div>
      </div>
    `).join('');

    this.shadowRoot.innerHTML = `
      <style>${STYLES}</style>
      <h1>ZabKiss — доступные устройства</h1>
      ${cards}
      <div class="save-bar">
        <button class="save-btn" id="save-btn">Сохранить</button>
        <span class="status" id="status-msg"></span>
      </div>
    `;
  }

  _attachListeners() {
    const root = this.shadowRoot;

    root.addEventListener('change', e => {
      const entity = e.target.dataset?.entity;
      if (!entity) return;
      e.target.checked ? this._enabled.add(entity) : this._enabled.delete(entity);
    });

    root.addEventListener('click', e => {
      const action = e.target.dataset?.action;
      if (!action) return;
      const card = e.target.closest('.card');
      if (!card) return;
      const checked = action === 'all';
      card.querySelectorAll('[data-entity]').forEach(cb => {
        cb.checked = checked;
        checked ? this._enabled.add(cb.dataset.entity) : this._enabled.delete(cb.dataset.entity);
      });
    });

    root.addEventListener('click', e => {
      if (e.target.id !== 'save-btn') return;
      this._save();
    });
  }

  async _save() {
    const root = this.shadowRoot;
    const btn = root.getElementById('save-btn');
    const msg = root.getElementById('status-msg');
    if (!btn) return;

    btn.disabled = true;
    msg.textContent = 'Сохранение...';

    try {
      await this._hass.callApi('POST', 'zabkiss/policy', {
        entities: [...this._enabled],
      });
      msg.textContent = 'Сохранено ✓';
    } catch (err) {
      msg.textContent = 'Ошибка: ' + (err?.message || 'неизвестная');
    } finally {
      btn.disabled = false;
      setTimeout(() => { if (msg) msg.textContent = ''; }, 3000);
    }
  }
}

if (!customElements.get('zabkiss-panel')) {
  customElements.define('zabkiss-panel', ZabKissPanel);
}
