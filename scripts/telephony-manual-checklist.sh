#!/usr/bin/env bash
# Чеклист жёстких ручных тестов softphone (блоки 1–4).
# Запуск: bash scripts/telephony-manual-checklist.sh
set -euo pipefail

cat <<'EOF'
=== Softphone hard tests — чеклист ===

Подготовка:
  [ ] Менеджер A (тестируемый) + B (второй номер)
  [ ] Softphone A: Vox ready, микрофон OK
  [ ] DevTools → Console + Network (softphone-event)
  [ ] Лид с реальным номером

Блок 1 — Исходящий / плашка / иконка
  [ ] 1.1 ready → dial с карточки: плашка «Набор…», TopBar pulse
  [ ] 1.2 idle → dial: connecting не «мёртвая» иконка
  [ ] 1.3 Connected: «Разговор» + таймер
  [ ] 1.4 гудки 30с: всё ещё «Набор…»
  [ ] 1.5 ×10 dial: плашка ≥9/10
  [ ] 1.6 лид / канбан / inbox одинаково

Блок 2 — Входящий во время исходящего
  [ ] 2.1 active + inbound: исходящий жив, НЕ «Входящий» UI
  [ ] 2.2 dialing + inbound: Busy в Console, набор жив
  [ ] 2.3 кнопка «Карточка» не пропадает
  [ ] 2.4 Network: softphone-event start с lead_id
  [ ] 2.5 Console: [vox] inbound rejected Busy
  [ ] 2.6 ×5: 5/5 исходящий жив

Блок 3 — Входящий в покое
  [ ] 3.1 ready + inbound: «Входящий», Answer/Decline
  [ ] 3.2 answer → hangup → «Завершён» / ready
  [ ] 3.3 missed: нет залипшего ringing

Блок 4 — Регресс
  [ ] 4.1 скорозвон: модалка, не обычная плашка
  [ ] 4.2 одна вкладка softphone
  [ ] 4.3 (опц.) после 4ч сессии — не кидает на /login

Провал: скрин + Console + softphone-event + lead_id + время.

Автотесты: node --test src/telephony/*.test.ts
EOF
