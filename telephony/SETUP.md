# Телефония Bitrix Isolation — Voximplant + МТС Exolve

В UI раздела **Телефония** можно переключать провайдера: **Voximplant** или **МТС Exolve**.  
Выбор вкладки руками сохраняется. Старый ключ, из‑за которого после `7925…` открывался Exolve, больше не читается — при заходе снова Voximplant (если в `.env` `VITE_TELEPHONY_PROVIDER=vox`).

---

## Общий стек в репозитории

| | Voximplant | МТС Exolve |
|--|------------|------------|
| Клиент | `src/telephony/voxClient.ts` | `src/telephony/exolveClient.ts` |
| SDK | `@voximplant/websdk` | `@mts-exolve/web-voice-sdk` |
| Сценарий | `telephony/scenarios/main.js` | не нужен |
| Credentials | user@app… + password | SIP login + password |

---

## МТС Exolve — быстрый старт

Документация: https://docs.exolve.ru/docs/ru/sdk-instructions/setting-up-web-sdk/

### Карусель DID (продакшен)

У каждого менеджера **свой SIP** и **свой пул номеров** (карусель только внутри пула):

1. В ЛК Exolve: купите DID, создайте SIP ID на менеджера, пароль SIP.
2. В CRM (админ) → **Телефония** → блок «Импорт пула Exolve» — JSON:

```json
{
  "managers": [
    {
      "email": "manager@company.ru",
      "sip_login": "88314…",
      "sip_password": "…",
      "numbers": ["7900…", "7900…"]
    }
  ]
}
```

3. Менеджер: вкладка **МТС Exolve** → Подключиться → «Проверить входящие» (SetCallForwarding всех DID → его SIP).
4. Исходящий: перед звонком API берёт следующий DID (LRU), вызывает `sip/v1/SetDisplayNumber`, затем softphone звонит. CLI виден в панели.
5. Нужен серверный `EXOLVE_API_KEY`. Без импорта пула работает **fallback** — общий `VITE_EXOLVE_SIP_*` / `EXOLVE_SIP_*` без карусели.

Миграция: `backend/migrations/016_exolve_carousel.sql` (`exolve_sips`, `exolve_dids`).

### Single-user / dev

1. Зарегистрируйтесь на [exolve.ru](https://exolve.ru/) → создайте приложение → API-ключ.
2. Купите номер (ЛК или Numbering API: GetFree → Lock → Buy).
3. Создайте SIP ID (ЛК «SIP-подключения» или `POST https://api.exolve.ru/sip/v1/Create`) с определяемым номером (CLI).
4. Возьмите `login` / `password` из карточки SIP или `sip/v1/GetAttributes`.
5. В `.env`:

```env
VITE_TELEPHONY_PROVIDER=exolve
VITE_EXOLVE_SIP_LOGIN=88314XXXXXXXXXX
VITE_EXOLVE_SIP_PASSWORD=********
VITE_EXOLVE_DEFAULT_DEST=79001234567

# Сервер: карусель + входящие (не VITE_*)
EXOLVE_API_KEY=********
# EXOLVE_SIP_LOGIN=88314XXXXXXXXXX   # optional; иначе VITE_EXOLVE_SIP_LOGIN
# EXOLVE_SIP_PASSWORD=********       # optional; иначе VITE_EXOLVE_SIP_PASSWORD
# EXOLVE_INBOUND_NUMBERS=79001234567  # optional список DID (legacy inbound)
```

6. `npm run dev` + API → **Телефония** → вкладка **МТС Exolve** → Подключиться.
7. Без пула: блок **Входящие** → «Принимать на softphone». С пулом: «Проверить входящие».
8. Softphone **На линии** → позвоните на DID с другого телефона → плашка «Ответить».

Бэкенд вызывает Exolve `SetCallForwarding` (тип 2 → SIP ID) и `SetDisplayNumber` для карусели. Без `EXOLVE_API_KEY` карусель/входящие из CRM недоступны; можно настроить то же в ЛК Exolve.

### Записи разговоров → история CRM

WAV **не** хранится в CRM — только `exolve_call_id` / `recording_key=exolve:{id}`. Плеер в карточке лида стримит через `GET /telephony/calls/{id}/recording` (прокси с `EXOLVE_API_KEY`).

В ЛК Exolve → приложение → **Уведомления о событиях (Callback)** на URL  
`https://<host>/webhooks/exolve/callback/<CALL_WEBHOOK_SECRET>`  
должны быть включены события звонка **и** **Call record ready** (`type: crr`). Запись разговоров на SIP/DID уже должна быть включена.

Формат набора: цифры `7XXXXXXXXXX` (из `+7` / `8` клиент нормализует сам).

Городской номер с вкладки Voximplant идёт в облачный сценарий (`telephony/scenarios/main.js`) через **callPSTN** с CLI номера, привязанного к приложению Vox. Вкладка МТС Exolve — отдельный контур.

---

## Voximplant — исходящие и входящие

Всё на стороне Voximplant (без Exolve SIP):

1. [Numbers](https://manage.voximplant.com/numbers) — номер → **Attach** к `bitrix-iso`.
2. Scenarios → вставить `telephony/scenarios/main.js`:
   - `DID_TO_USER` — каждый DID → свой short user (`agent1`…`agent101`)
   - исходящий CLI берётся из того же DID по юзеру, который звонит
   - в Vox Users создайте `agent1`…`agent101`, в CRM привяжите логин/пароль сотруднику
3. Routing rule `.*` → сценарий → Save.
4. `.env`: только `VITE_VOX_USERNAME` / `VITE_VOX_PASSWORD` / `VITE_VOX_NODE`.
5. CRM → **Voximplant** → Подключиться → звонки на город и приём на DID.

МТС Exolve — отдельная вкладка softphone, свои DID/SIP.

Тест user↔user: https://phone.voximplant.com/

---

## Credentials / секреты

- Файлы `*_private.json`, `.env` — в `.gitignore`, не коммитить.
- Service account Vox — для Management API / деплоя сценария.
- Exolve API-ключ (`EXOLVE_API_KEY`) — на сервере для карусели CLI (`SetDisplayNumber`), переадресации входящих и импорта пула; softphone в браузере получает SIP login/password из `GET /telephony/exolve/session` (личный пул) или fallback `VITE_EXOLVE_*`.
