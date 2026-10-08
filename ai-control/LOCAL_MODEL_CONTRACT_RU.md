# Контракт local model adapter

`AI_MODEL_MODE=local` разрешает только явно заданный `http://localhost`, `http://127.0.0.1` или `http://[::1]` endpoint из конфигурации. URL из conversation или пользовательского файла не используется.

Сервис отправляет `POST` с JSON conversation. Endpoint должен вернуть JSON `AnalysisResult` не больше 1 MiB. Сервер заново устанавливает `analysis_id`, `job_id`, source metadata, mode, status и created_at; эти поля ответа модели не считаются доверенными.

Каждый `message_id` в `evidence` обязан существовать во входной conversation. Неизвестная ссылка, невалидный JSON, HTTP не-2xx, недоступность endpoint или timeout превращаются в safe `model_error`; fallback в mock или облако не выполняется.

Модель и веса в эту папку не включаются и автоматически не скачиваются. Перед подключением реального runtime нужно отдельно подтвердить выбранную модель, ресурсный бюджет и команду запуска.
