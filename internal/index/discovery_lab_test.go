package index

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/smart-mcp-proxy/mcpproxy-go/internal/config"
	"go.uber.org/zap"
)

// Real tool descriptions captured from prod (dimba-mcp-gateway) on 2026-04-17.
// Source: mcp__dimba-mcp__retrieve_tools with various thematic queries.
// Kept inline so the test is self-contained and runnable without external files.
func realProdTools() []*config.ToolMetadata {
	now := time.Now()
	tools := []*labTool{
		// b24 — CRM
		{"b24", "list_items_tool", "List CRM items for any entity type (deals, contacts, companies, invoices, smart processes). Filter and order are JSON strings. Filter supports prefixes: >=, >, <=, <, =, !=, %, @. SEARCH: Use %searchContent for full-text search on companies and contacts — it indexes title, phones, emails, INN, and other fields. For deals/invoices, search by companyId or contactId. Common filter fields — Deals: stageId, categoryId, companyId, contactId, assignedById, opportunity, begindate, closedate. Contacts: name, lastName, phone, email. Dates use ISO format."},
		{"b24", "get_item_tool", "Get single CRM item with enriched details, product rows, custom fields"},
		{"b24", "create_item_tool", "Create a CRM item. Fields is a JSON string with field values. Supports productRows for any entity with products (deals, invoices, smart processes). Examples: fields='{\"title\": \"New deal\", \"stageId\": \"NEW\"}'"},
		{"b24", "update_item_tool", "Update a CRM item by ID. Fields is a JSON string with fields to change. Supports productRows — replaces all product rows for the entity."},
		{"b24", "list_crm_statuses_tool", "List CRM reference book (справочник) values via crm.status.list. entity_id: Reference type to query. Common values: SOURCE — lead/deal sources (Источник), CONTACT_TYPE — contact types, COMPANY_TYPE — company types, INDUSTRY — industries, EMPLOYEES — employee count ranges, DEAL_TYPE — deal types, DEAL_STAGE — deal stages (default pipeline), DEAL_STAGE_xx — deal stages for pipeline xx, QUOTE_STATUS — quote statuses. If empty — returns ALL references grouped by type."},
		{"b24", "get_crm_types_tool", "Get available CRM entity types with their entityTypeId. Standard: Leads=1, Deals=2, Contacts=3, Companies=4, Invoices=31."},
		{"b24", "get_fields_tool", "Get fields for a CRM entity type. Shows field names, types, required/readonly flags, and allowed values for enum fields."},
		{"b24", "get_pipelines_tool", "Get pipelines (categories) and their stages for an entity type. Useful for understanding deal flow and available stage transitions."},
		{"b24", "list_requisites_tool", "List requisites (legal details) for a CRM entity. entity_type_id: CRM entity type (4=company, 3=contact, etc.). Returns: INN, KPP, OGRN, company name, director, accountant for each requisite."},
		{"b24", "get_requisite_tool", "Get a single requisite by ID with bank details and addresses. Returns full legal info (INN, KPP, OGRN, company name), bank details (BIK, account number, correspondent account), and registered addresses."},
		{"b24", "create_requisite_tool", "Create a requisite (legal details) for a company or contact. Key fields: NAME, RQ_INN, RQ_KPP, RQ_OGRN, RQ_COMPANY_FULL_NAME, RQ_DIRECTOR, RQ_ACCOUNTANT."},
		// b24 — documents
		{"b24", "list_document_templates_tool", "List available document templates (invoice, act, contract, etc.) for a CRM entity type. entity_type_id: CRM entity type (2=deal, 31=smart invoice, 3=contact, 4=company). Returns template id and name."},
		{"b24", "list_documents_tool", "List generated documents (acts, invoices, contracts) for CRM entities. Uses crm.documentgenerator.document.list. Efficient batch for multiple entities."},
		{"b24", "create_document_tool", "Generate a new document (invoice, act, contract) from a template. template_id: Document template ID. entity_type_id: CRM entity type (2=deal, 31=smart invoice)."},
		// b24 — activities/tasks
		{"b24", "list_activities_tool", "List CRM activities (timeline items): calls, emails, meetings, tasks, todos. Activity types (TYPE_ID): 1=Meeting, 2=Call, 3=Task, 4=Email. Owner types (OWNER_TYPE_ID): 1=Lead, 2=Deal, 3=Contact, 4=Company."},
		{"b24", "get_activity_tool", "Get a single CRM activity by ID with full details. Returns type, subject, description, owner, responsible, communications, dates."},
		{"b24", "complete_activity_tool", "Mark a CRM activity as completed."},
		{"b24", "update_activity_tool", "Update a CRM activity. Fields is a JSON string. Common fields: SUBJECT, DESCRIPTION, DEADLINE, RESPONSIBLE_ID, PRIORITY."},
		{"b24", "delete_activity_tool", "Delete a CRM activity by ID."},
		{"b24", "add_todo_tool", "Create a universal todo activity (recommended way to create activities). owner_type_id: 1=Lead, 2=Deal, 3=Contact, 4=Company. deadline: Due date in ISO format."},
		{"b24", "list_tasks_tool", "List Bitrix24 tasks. Filter fields: ID, TITLE, STATUS, PRIORITY, RESPONSIBLE_ID, CREATED_BY, GROUP_ID, TAG, DEADLINE. Status codes: 2=pending, 3=in progress, 4=awaiting control, 5=completed."},
		{"b24", "create_task_tool", "Create a Bitrix24 task. Fields is a JSON string. Required: TITLE. Optional: DESCRIPTION, RESPONSIBLE_ID, DEADLINE, GROUP_ID, PRIORITY, ACCOMPLICES, AUDITORS, TAGS."},
		{"b24", "get_task_tool", "Get a single task by ID with full details. Includes description, accomplices, auditors, time tracking, CRM bindings, and mark."},
		{"b24", "list_elapsed_tool", "Get time spent (elapsed) records — logged work hours. BY DATE (preferred): pass filter with date range. BY TASKS: pass task_ids array. Fields: USER_ID, CREATED_DATE."},
		{"b24", "add_elapsed_tool", "Add a time spent record to a task. task_id, seconds (3600 = 1 hour), comment, created_date."},
		{"b24", "update_elapsed_tool", "Update an existing time spent record. task_id, item_id (Elapsed record ID), seconds, comment."},
		// b24 — portals/calendar/bizproc
		{"b24", "list_portals_tool", "List all configured Bitrix24 portals and show which one is active."},
		{"b24", "switch_portal_tool", "Switch the active Bitrix24 portal."},
		{"b24", "list_events_tool", "List calendar events for a user, group, or company. owner_id: User/group ID. cal_type: 'user', 'group', or 'company_calendar'. Returns events sorted by date with name, time, location, attendees, CRM bindings."},
		{"b24", "bp_list_tool", "List bizproc (workflow) templates — business processes and robots. entity_type: DEAL, LEAD, CONTACT, COMPANY. bp_type: 'bp' (manual/auto BPs) or 'robot' (automation rules)."},
		{"b24", "bp_get_tool", "Get a bizproc template structure. format: 'summary' (compact tree), 'yaml' (full activity tree), 'tree' (yaml without props), 'json' (raw API response)."},
		{"b24", "bp_search_tool", "Search activities inside a BP template by title and return them with full props."},
		// dadata
		{"dadata", "find_party", "Находит компанию или индивидуального предпринимателя по ИНН или ОГРН. Возвращает реквизиты компании, финансовые показатели и другую информацию о компании. Полезен для анализа компаний и контрагентов."},
		{"dadata", "clean_address", "Исправляет ошибки в почтовом адресе и стандартизирует его, обогащает координатами, кодами ФИАС, КЛАДР и другой полезной информацией."},
		{"dadata", "find_company_by_email", "Находит компанию или индивидуального предпринимателя по электронной почте. Возвращает базовые реквизиты компании."},
		{"dadata", "find_company_by_domain", "Находит компанию или индивидуального предпринимателя по домену сайта компании."},
		// beget
		{"beget", "account_info", "Get Beget hosting account info: plan, quotas, disk usage, bandwidth."},
		{"beget", "backup_files_list", "List available file backups on Beget hosting."},
		{"beget", "backup_mysql_list", "List available MySQL database backups."},
		{"beget", "backup_restore_file", "Restore files from backup by backup_id and path."},
		{"beget", "backup_restore_mysql", "Restore a MySQL database from backup by backup_id and database name."},
		{"beget", "dns_set_a", "Задать A-запись (IPv4) для домена. Перезаписывает прежние A-записи."},
		{"beget", "cron_delete", "Убрать cron-задачу. Args: task_id."},
		{"beget", "cron_edit", "Изменить параметры cron-задачи. Args: task_id, minutes, hours, days, months, weekdays, command."},
		{"beget", "cron_get_email", "Адрес уведомлений о выполнении cron-задач."},
		{"beget", "cron_set_email", "Назначить email для отчётов cron. Пустая строка отключает уведомления."},
		{"beget", "mail_forward_add", "Включить пересылку на указанный адрес. Args: domain, mailbox, forward_mailbox."},
		{"beget", "mail_forward_delete", "Снять пересылку на указанный адрес."},
	}

	result := make([]*config.ToolMetadata, 0, len(tools))
	for i, t := range tools {
		result = append(result, &config.ToolMetadata{
			Name:        t.server + ":" + t.tool,
			ServerName:  t.server,
			Description: t.desc,
			ParamsJSON:  `{"type":"object"}`,
			Hash:        fmt.Sprintf("real-%s-%s-%d", t.server, t.tool, i),
			Created:     now,
			Updated:     now,
		})
	}
	return result
}

type labTool struct {
	server string
	tool   string
	desc   string
}

// aliasesConfig is the proposed per-server + per-tool override map.
// Empty => baseline (no aliases).
type aliasesConfig map[string]serverAliases

type serverAliases struct {
	// Applied to every tool on this server.
	serverLevel []string
	// Per-tool, merged with serverLevel.
	perTool map[string][]string
}

// applyAliases mutates tool descriptions by appending configured aliases.
// This emulates the future 'aliases' bleve field with a boost, but using the
// existing description field — enough to demonstrate ranking impact.
// The aliases are repeated 3× to roughly emulate a boost=3 multiplier.
func applyAliases(tools []*config.ToolMetadata, cfg aliasesConfig) []*config.ToolMetadata {
	out := make([]*config.ToolMetadata, len(tools))
	for i, orig := range tools {
		tname := strings.TrimPrefix(orig.Name, orig.ServerName+":")
		var extras []string
		if s, ok := cfg[orig.ServerName]; ok {
			extras = append(extras, s.serverLevel...)
			if tl, ok := s.perTool[tname]; ok {
				extras = append(extras, tl...)
			}
		}
		copyOf := *orig
		if len(extras) > 0 {
			boosted := strings.Join(extras, " ")
			// Repeat 3× to emulate boost.
			copyOf.Description = orig.Description + " " + boosted + " " + boosted + " " + boosted
		}
		out[i] = &copyOf
	}
	return out
}

type labQuery struct {
	query    string
	expected []string // full tool names (server:tool), any match in top-K counts as hit
}

func discoveryGroundTruth() []labQuery {
	return []labQuery{
		// Russian, cross-language
		{"последние сделки битрикс24", []string{"b24:list_items_tool"}},
		{"список сделок", []string{"b24:list_items_tool"}},
		{"создать контакт", []string{"b24:create_item_tool"}},
		{"найти контрагента по ИНН", []string{"dadata:find_party", "b24:list_requisites_tool"}},
		{"реквизиты компании", []string{"b24:list_requisites_tool", "b24:get_requisite_tool", "dadata:find_party"}},
		{"шаблоны документов счёт акт", []string{"b24:list_document_templates_tool", "b24:list_documents_tool"}},
		{"открытые задачи в работе", []string{"b24:list_tasks_tool"}},
		{"воронка продаж стадии", []string{"b24:get_pipelines_tool", "b24:list_crm_statuses_tool"}},
		{"календарь встречи на неделю", []string{"b24:list_events_tool"}},
		{"список бэкапов mysql", []string{"beget:backup_mysql_list"}},
		{"восстановить базу данных из бэкапа", []string{"beget:backup_restore_mysql"}},
		{"почтовая пересылка домена", []string{"beget:mail_forward_add"}},
		{"стандартизировать почтовый адрес", []string{"dadata:clean_address"}},
		{"учёт рабочего времени по задачам", []string{"b24:list_elapsed_tool"}},
		// English
		{"list deals by company", []string{"b24:list_items_tool"}},
		{"create crm deal", []string{"b24:create_item_tool"}},
		{"find company by INN", []string{"dadata:find_party", "b24:list_requisites_tool"}},
		{"mysql database backups", []string{"beget:backup_mysql_list"}},
		{"generate invoice document", []string{"b24:create_document_tool", "b24:list_document_templates_tool"}},
		{"cron jobs notifications", []string{"beget:cron_set_email", "beget:cron_get_email"}},
	}
}

func productionAliases() aliasesConfig {
	return aliasesConfig{
		"b24": {
			serverLevel: []string{"битрикс", "битрикс24", "bitrix24", "b24", "crm", "портал"},
			perTool: map[string][]string{
				"list_items_tool":             {"сделки", "deals", "лиды", "leads", "контакты", "contacts", "компании", "companies", "список сделок", "recent deals", "список лидов"},
				"get_item_tool":               {"карточка", "card", "детали элемента", "item details", "get one by id"},
				"create_item_tool":            {"создать сделку", "создать контакт", "создать лид", "создать компанию", "new deal", "create contact", "create lead"},
				"update_item_tool":            {"обновить сделку", "сменить стадию", "change stage", "update deal"},
				"list_requisites_tool":        {"реквизиты", "requisites", "ИНН", "INN", "КПП", "ОГРН", "юридические данные"},
				"get_requisite_tool":          {"реквизит", "банковские реквизиты", "bank details"},
				"list_document_templates_tool": {"шаблоны", "templates", "счёт", "invoice", "акт", "act", "договор", "contract"},
				"list_documents_tool":          {"документы", "documents", "счета", "invoices"},
				"create_document_tool":         {"создать счёт", "generate invoice", "сгенерировать документ"},
				"list_tasks_tool":              {"задачи", "tasks", "открытые задачи", "in progress"},
				"create_task_tool":             {"создать задачу", "new task", "поставить задачу"},
				"list_activities_tool":         {"активности", "звонки", "calls", "письма", "emails", "meetings", "встречи", "timeline"},
				"get_pipelines_tool":           {"воронка", "pipeline", "стадии", "stages", "воронки продаж"},
				"list_crm_statuses_tool":       {"справочники", "reference books", "статусы"},
				"list_events_tool":             {"календарь", "calendar", "встречи", "events", "расписание"},
				"list_elapsed_tool":            {"учёт времени", "time tracking", "списанные часы", "рабочее время"},
				"bp_list_tool":                 {"бизнес-процессы", "роботы", "robots", "workflows"},
			},
		},
		"dadata": {
			serverLevel: []string{"дадата", "dadata", "контрагенты", "counterparty", "справочные"},
			perTool: map[string][]string{
				"find_party":             {"поиск по ИНН", "find by INN", "ОГРН", "реквизиты компании", "контрагент"},
				"clean_address":          {"почтовый адрес", "address cleanup", "стандартизация", "КЛАДР", "ФИАС"},
				"find_company_by_email":  {"компания по email", "company by email"},
				"find_company_by_domain": {"компания по домену", "company by domain"},
			},
		},
		"beget": {
			serverLevel: []string{"бегет", "beget", "хостинг", "hosting", "vps"},
			perTool: map[string][]string{
				"backup_mysql_list":    {"бэкапы mysql", "mysql backups", "резервные копии базы"},
				"backup_files_list":    {"бэкапы файлов", "file backups"},
				"backup_restore_mysql": {"восстановить mysql", "restore database", "восстановить базу", "база данных из бэкапа"},
				"backup_restore_file":  {"восстановить файлы", "restore files"},
				"mail_forward_add":     {"пересылка почты", "mail forward"},
				"mail_forward_delete":  {"отключить пересылку", "remove forward"},
				"cron_set_email":       {"уведомления cron", "cron notifications"},
				"dns_set_a":            {"A-запись", "DNS record"},
			},
		},
	}
}

// runScenario indexes the given tools and returns per-query top-K results.
type scenarioResult struct {
	query    string
	expected []string
	topK     []string // tool full names in rank order
}

func runScenario(t *testing.T, tools []*config.ToolMetadata, queries []labQuery, topK int) []scenarioResult {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "lab_bleve_*")
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	idx, err := NewBleveIndex(tmpDir, zap.NewNop())
	if err != nil {
		t.Fatalf("bleve new: %v", err)
	}
	defer idx.Close()

	if err := idx.BatchIndex(tools); err != nil {
		t.Fatalf("batch index: %v", err)
	}

	out := make([]scenarioResult, 0, len(queries))
	for _, q := range queries {
		hits, err := idx.SearchTools(q.query, topK)
		if err != nil {
			t.Fatalf("search %q: %v", q.query, err)
		}
		names := make([]string, 0, len(hits))
		for _, h := range hits {
			names = append(names, h.Tool.Name)
		}
		out = append(out, scenarioResult{query: q.query, expected: q.expected, topK: names})
	}
	return out
}

type metrics struct {
	hitAt1  int
	hitAt3  int
	hitAt5  int
	totalMR float64 // sum of reciprocal ranks; divide by N for MRR
	zeros   int
	total   int
}

func computeMetrics(results []scenarioResult) metrics {
	m := metrics{total: len(results)}
	for _, r := range results {
		if len(r.topK) == 0 {
			m.zeros++
			continue
		}
		rank := -1
		for i, n := range r.topK {
			for _, exp := range r.expected {
				if n == exp {
					rank = i + 1
					break
				}
			}
			if rank != -1 {
				break
			}
		}
		if rank == -1 {
			continue
		}
		if rank == 1 {
			m.hitAt1++
		}
		if rank <= 3 {
			m.hitAt3++
		}
		if rank <= 5 {
			m.hitAt5++
		}
		m.totalMR += 1.0 / float64(rank)
	}
	return m
}

func printReport(t *testing.T, label string, results []scenarioResult, m metrics) {
	t.Helper()
	t.Logf("=== %s ===", label)
	// Sort alphabetically for readable diff.
	sorted := make([]scenarioResult, len(results))
	copy(sorted, results)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].query < sorted[j].query })
	for _, r := range sorted {
		hit := "MISS"
		rank := -1
		for i, n := range r.topK {
			for _, exp := range r.expected {
				if n == exp {
					rank = i + 1
					break
				}
			}
			if rank != -1 {
				break
			}
		}
		if rank != -1 {
			hit = fmt.Sprintf("hit@%d", rank)
		}
		topPreview := strings.Join(r.topK[:min(3, len(r.topK))], ", ")
		t.Logf("  [%s] %-45s  expected={%s}  top3=[%s]", hit, r.query, strings.Join(r.expected, ","), topPreview)
	}
	t.Logf("  --")
	t.Logf("  hit@1 = %d/%d (%.0f%%)", m.hitAt1, m.total, 100*float64(m.hitAt1)/float64(m.total))
	t.Logf("  hit@3 = %d/%d (%.0f%%)", m.hitAt3, m.total, 100*float64(m.hitAt3)/float64(m.total))
	t.Logf("  hit@5 = %d/%d (%.0f%%)", m.hitAt5, m.total, 100*float64(m.hitAt5)/float64(m.total))
	t.Logf("  MRR   = %.3f", m.totalMR/float64(m.total))
	t.Logf("  zero-results = %d/%d", m.zeros, m.total)
}

// TestDiscoveryLab_RealProd_Baseline: measures current BM25 behaviour on the
// snapshot of real prod tools with ZERO aliases. Establishes a baseline to
// compare against when config aliases (Component 1+2 of the 2026-04-17 spec)
// are layered on top.
func TestDiscoveryLab_RealProd_Baseline(t *testing.T) {
	tools := realProdTools()
	queries := discoveryGroundTruth()
	results := runScenario(t, tools, queries, 5)
	m := computeMetrics(results)
	printReport(t, "BASELINE (no aliases, real prod descriptions)", results, m)
}

// TestDiscoveryLab_RealProd_WithAliases: measures BM25 after layering config
// aliases (server_aliases + tool_aliases) into the indexed description.
// This emulates the proposed 'aliases' field with boost=3.
func TestDiscoveryLab_RealProd_WithAliases(t *testing.T) {
	tools := applyAliases(realProdTools(), productionAliases())
	queries := discoveryGroundTruth()
	results := runScenario(t, tools, queries, 5)
	m := computeMetrics(results)
	printReport(t, "WITH ALIASES (server+tool aliases, boost=3× via repetition)", results, m)
}
