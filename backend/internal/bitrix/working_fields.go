package bitrix

type FieldSpec struct {
	Code  string
	Label string
	Group string
	Sort  int
}

const (
	groupHire    = "Найм"
	groupDocs    = "Документы"
	groupTrip    = "Логистика"
	groupChan    = "Каналы"
	groupSrc     = "Источники"
	groupRoute   = "Раздача"
)

func spec(code, label, group string, sort int) FieldSpec {
	return FieldSpec{Code: code, Label: label, Group: group, Sort: sort}
}

// WorkingDealFields — поля живой карточки найма. Хвост проектирования не входит.
func WorkingDealFields() []FieldSpec {
	s := 10
	next := func() int { s += 10; return s }
	return []FieldSpec{
		spec("UF_CRM_1761381063703", "Вакансия", groupHire, next()),
		spec("UF_CRM_1761381399980", "Возраст", groupHire, next()),
		spec("UF_CRM_1761381455236", "Финансовые ожидания кандидата", groupHire, next()),
		spec("UF_CRM_TIMEZONE_PROFIT", "Часовой пояс", groupHire, next()),
		spec("UF_CRM_PROFIT_TIME_NOW", "Время клиента", groupHire, next()),
		spec("UF_CRM_1770117426785", "Время клиента", groupHire, next()),
		spec("UF_CRM_1761381805640", "Дата и время интервью", groupHire, next()),
		spec("UF_CRM_1761382518171", "Дата и время повторного интервью", groupHire, next()),
		spec("UF_CRM_1779173915866", "Формат интервью с кандидатом", groupHire, next()),
		spec("UF_CRM_1779173942691", "Дата интервью с кандидатом", groupHire, next()),
		spec("UF_CRM_1779173956596", "Дата собеседования с кандидатом", groupHire, next()),
		spec("UF_CRM_1779173985550", "Формат собеседования с кандидатом", groupHire, next()),
		spec("UF_CRM_1779174376", "Итоги интервью с кандидатом", groupHire, next()),
		spec("UF_CRM_1779174394", "Итоги собеседования с кандидатом", groupHire, next()),
		spec("UF_CRM_1761381832207", "Комментарии для рекрутера", groupHire, next()),
		spec("UF_CRM_1761382660445", "Комментарии по кандидату", groupHire, next()),
		spec("UF_CRM_1769699745498", "Комментарий", groupHire, next()),
		spec("UF_CRM_1761382620823", "Дата выхода к работодателю", groupHire, next()),
		spec("UF_CRM_1779174143128", "Дата выхода на стажировку", groupHire, next()),
		spec("UF_CRM_1761383072958", "Оффер", groupHire, next()),
		spec("UF_CRM_1779174131128", "Оффер кандидату", groupHire, next()),
		spec("UF_CRM_1761381377827", "Резюме", groupHire, next()),
		spec("UF_CRM_1779173770040", "Резюме", groupHire, next()),
		spec("UF_CRM_1779170845427", "Время назначенного звонка", groupHire, next()),
		spec("UF_CRM_1782752002", "Наблюдатель", groupHire, next()),
		spec("UF_CRM_DEAL_1780997804862", "Город проживания", groupHire, next()),
		spec("UF_CRM_1783451105153", "Город внесения", groupHire, next()),
		spec("UF_CRM_1768570439334", "Дата закрытия", groupHire, next()),
		spec("UF_CRM_1768570448237", "Город закрытия", groupHire, next()),
		spec("UF_CRM_1771603270918", "Из КЦ", groupHire, next()),
		spec("UF_CRM_1771934689665", "Какая форма", groupHire, next()),
		spec("UF_CRM_1780569815139", "Оператор КЦ (ЕКБ)", groupHire, next()),

		spec("UF_CRM_1761383048554", "Паспорт кандидата", groupDocs, next()),
		spec("UF_CRM_1769290431356", "Серия и номер паспорта", groupDocs, next()),
		spec("UF_CRM_1769290522780", "Ф.И.О. по паспорту", groupDocs, next()),
		spec("UF_CRM_1769290422976", "ИНН", groupDocs, next()),
		spec("UF_CRM_1769290544847", "СНИЛС", groupDocs, next()),
		spec("UF_CRM_1769290560268", "Дата рождения", groupDocs, next()),
		spec("UF_CRM_1769290467988", "Сканы / фото документов", groupDocs, next()),
		spec("UF_CRM_1761383741078", "Подтверждение по кандидату", groupDocs, next()),
		spec("UF_CRM_1761825378263", "Подтверждение по кандидату (выплата)", groupDocs, next()),

		spec("UF_CRM_1761382772991", "Заказ билетов", groupTrip, next()),
		spec("UF_CRM_1761382905749", "Желаемая дата отправки", groupTrip, next()),
		spec("UF_CRM_1761382990897", "Нужно ли жилье", groupTrip, next()),
		spec("UF_CRM_1761386967772", "Точка отправки", groupTrip, next()),
		spec("UF_CRM_1761386992871", "Точка прибытия", groupTrip, next()),
		spec("UF_CRM_1769291085583", "Билет: откуда", groupTrip, next()),
		spec("UF_CRM_1769291094494", "Билет: куда", groupTrip, next()),
		spec("UF_CRM_1769385661", "Билеты", groupTrip, next()),
		spec("UF_CRM_1769699734243", "Дата и время прибытия кандидата", groupTrip, next()),

		spec("UF_CRM_69205D091A225", "MAX", groupChan, next()),
		spec("UF_CRM_MAXID_WZ", "MAX", groupChan, next()),
		spec("UF_CRM_MAXUSERNAME_WZ", "MAX (имя)", groupChan, next()),
		spec("UF_CRM_69BD09BBD1F4E", "WhatsApp", groupChan, next()),
		spec("UF_CRM_69BD09BBE1E48", "WhatsApp LID", groupChan, next()),
		spec("UF_CRM_WHATSAPPUSERNAME_WZ", "WhatsApp", groupChan, next()),
		spec("UF_CRM_WHATSAPPLID_WZ", "WhatsApp LID", groupChan, next()),
		spec("UF_CRM_69205D091DB73", "Telegram", groupChan, next()),
		spec("UF_CRM_69205D0920A8E", "Telegram ID", groupChan, next()),
		spec("UF_CRM_TELEGRAMUSERNAME_WZ", "Telegram", groupChan, next()),
		spec("UF_CRM_TELEGRAMID_WZ", "Telegram ID", groupChan, next()),
		spec("UF_CRM_69205D0911C58", "Avito", groupChan, next()),
		spec("UF_CRM_AVITO_WZ", "Avito", groupChan, next()),
		spec("UF_CRM_69205D090EAC2", "VK", groupChan, next()),
		spec("UF_CRM_VK_WZ", "VK", groupChan, next()),
		spec("UF_CRM_69205D0907F5E", "Instagram", groupChan, next()),
		spec("UF_CRM_INSTAGRAM_WZ", "Instagram", groupChan, next()),

		spec("UF_CRM_DEAL_HH2CRM_VACANCY", "Вакансия HH", groupSrc, next()),
		spec("UF_CRM_DEAL_HH2CRM_NEGOTIATION", "Отклик HH", groupSrc, next()),
		spec("UF_CRM_DEAL_HH2CRM_EMPLOYEE", "Аккаунт HH", groupSrc, next()),
		spec("UF_CRM_AVITO_DUPLICATES", "Вакансия Avito", groupSrc, next()),
		spec("UF_CRM_1763630597570", "ID кампании", groupSrc, next()),
		spec("UF_CRM_1763630608161", "ID группы", groupSrc, next()),
		spec("UF_CRM_1763663337106", "Время создания лида", groupSrc, next()),
		spec("UF_CRM_1779747328", "Форма ВК", groupSrc, next()),

		spec("UF_CRM_1769209340830", "Ответственный с квалификации", groupRoute, next()),
		spec("UF_CRM_1776208122", "Старый ответственный", groupRoute, next()),
		spec("UF_CRM_1776244317", "Ответственный для заявки КЦ", groupRoute, next()),
		spec("UF_CRM_1776247947", "Ответственный менеджер из квалификации", groupRoute, next()),
	}
}

func WorkingLeadFields() []FieldSpec {
	return []FieldSpec{
		spec("UF_CRM_PROFIT_TIME_NOW", "Время клиента", groupHire, 10),
		spec("UF_CRM_INSTAGRAM_WZ", "Instagram", groupChan, 20),
		spec("UF_CRM_VK_WZ", "VK", groupChan, 30),
		spec("UF_CRM_AVITO_WZ", "Avito", groupChan, 40),
		spec("UF_CRM_MAXID_WZ", "MAX", groupChan, 50),
		spec("UF_CRM_TELEGRAMUSERNAME_WZ", "Telegram", groupChan, 60),
		spec("UF_CRM_TELEGRAMID_WZ", "Telegram ID", groupChan, 70),
		spec("UF_CRM_WHATSAPPUSERNAME_WZ", "WhatsApp", groupChan, 80),
		spec("UF_CRM_WHATSAPPLID_WZ", "WhatsApp LID", groupChan, 90),
		spec("UF_CRM_LEAD_HH2CRM_VACANCY", "Вакансия HH", groupSrc, 100),
		spec("UF_CRM_LEAD_HH2CRM_NEGOTIATION", "Отклик HH", groupSrc, 110),
		spec("UF_CRM_LEAD_HH2CRM_EMPLOYEE", "Аккаунт HH", groupSrc, 120),
		spec("UF_CRM_AVITO_DUPLICATES", "Вакансия Avito", groupSrc, 130),
		spec("UF_CRM_CIANPROFILEID_WZ", "Циан", groupChan, 140),
		spec("UF_CRM_SASHA_DUP_LEAD", "Дубль в колл-листе", groupRoute, 150),
	}
}

func WorkingContactFields() []FieldSpec {
	return []FieldSpec{
		spec("UF_CRM_PROFIT_TIME_NOW", "Время клиента", groupHire, 10),
		spec("UF_CRM_INSTAGRAM_WZ", "Instagram", groupChan, 20),
		spec("UF_CRM_TELEGRAMUSERNAME_WZ", "Telegram", groupChan, 30),
		spec("UF_CRM_VK_WZ", "VK", groupChan, 40),
		spec("UF_CRM_AVITO_WZ", "Avito", groupChan, 50),
		spec("UF_CRM_MAXID_WZ", "MAX", groupChan, 60),
		spec("UF_CRM_TELEGRAMID_WZ", "Telegram ID", groupChan, 70),
		spec("UF_CRM_WHATSAPPUSERNAME_WZ", "WhatsApp", groupChan, 80),
		spec("UF_CRM_WHATSAPPLID_WZ", "WhatsApp LID", groupChan, 90),
		spec("UF_CRM_LEAD_HH2CRM_VACANCY", "Вакансия HH", groupSrc, 100),
		spec("UF_CRM_CONTACT_HH2CRM_VACANCY", "Вакансия HH", groupSrc, 101),
		spec("UF_CRM_LEAD_HH2CRM_NEGOTIATION", "Отклик HH", groupSrc, 110),
		spec("UF_CRM_AVITO_DUPLICATES", "Вакансия Avito", groupSrc, 130),
		spec("UF_CRM_CIANPROFILEID_WZ", "Циан", groupChan, 140),
		spec("UF_CRM_SASHA_DUP_LEAD", "Дубль в колл-листе", groupRoute, 150),
	}
}

func fieldCatalog(entity string) []FieldSpec {
	switch entity {
	case "lead":
		return WorkingLeadFields()
	case "contact":
		return WorkingContactFields()
	default:
		return WorkingDealFields()
	}
}

func fieldIndex(list []FieldSpec) map[string]FieldSpec {
	out := make(map[string]FieldSpec, len(list))
	for _, f := range list {
		out[f.Code] = f
	}
	return out
}
