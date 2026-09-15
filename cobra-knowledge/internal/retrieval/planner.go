package retrieval

import (
	"fmt"
	"strings"

	"cobraknowledge.local/cobra-knowledge/internal/model"
)

type Planner struct{ Catalog SemanticCatalog }

func NewPlanner(c SemanticCatalog) *Planner { return &Planner{Catalog: c} }

var definitionWords = []string{"什么是", "定义", "含义", "概念", "区别", "解释"}
var reasonWords = []string{"为什么", "原因", "依据", "说明", "如何判断", "怎么判断", "诊断", "分析原因"}
var currentWords = []string{"当前", "现在", "实时", "最新", "现状", "目前"}
var schemaWords = []string{"本体", "类层级", "domain", "range", "合法关系", "允许关系", "类型关系", "schema"}
var complexWords = []string{"综合", "结合", "影响哪些", "怎么关联", "路径", "多跳", "以及", "同时", "并且"}
var metricTaskWords = []string{"问数", "指标", "数据查询", "metric", "data_query"}

type sourceChoice struct {
	required bool
	reason   string
}

func (p *Planner) Plan(req model.QueryRequest) model.RetrievalPlan {
	q := strings.TrimSpace(req.Query)
	semantics := model.QuerySemantics{Intent: "fact_query", Scope: cloneScope(req.Scope)}
	choices := map[model.RetrievalSource]sourceChoice{}
	targetTerms := []string{}

	for _, term := range p.Catalog.Classes {
		if termMatches(q, term) {
			semantics.MatchedClasses = append(semantics.MatchedClasses, term.ID)
			targetTerms = append(targetTerms, term.Label)
		}
	}
	for _, term := range p.Catalog.Properties {
		if termMatches(q, term) {
			semantics.MatchedProperties = append(semantics.MatchedProperties, term.ID)
			targetTerms = append(targetTerms, term.Label)
		}
	}
	for _, term := range p.Catalog.Relations {
		if termMatches(q, term) {
			semantics.MatchedRelations = append(semantics.MatchedRelations, term.ID)
			targetTerms = append(targetTerms, term.Label)
		}
	}
	targetTerms = uniqueStrings(targetTerms)

	add := func(source model.RetrievalSource, required bool, reason string) {
		if source == "" {
			return
		}
		current, ok := choices[source]
		if !ok {
			choices[source] = sourceChoice{required: required, reason: reason}
			return
		}
		current.required = current.required || required
		if current.reason == "" {
			current.reason = reason
		}
		choices[source] = current
	}

	isSchema := containsAny(q, schemaWords)
	isDefinition := containsAny(q, definitionWords)
	isDiagnosis := containsAny(q, reasonWords)
	isCurrent := containsAny(q, currentWords)
	isMetricTask := containsAny(strings.ToLower(req.Task), metricTaskWords)
	businessPreferred := p.matchedPropertyPrefers(q, model.SourceBusinessData)

	switch {
	case isSchema:
		semantics.Intent = "schema_query"
		add(model.SourceOntologyGraph, true, "问题直接涉及本体、类型或关系约束")
	case isDefinition:
		semantics.Intent = "definition"
		add(model.SourceWikiRAG, true, "定义和解释优先使用正式文档知识")
		if len(semantics.MatchedClasses)+len(semantics.MatchedProperties)+len(semantics.MatchedRelations) > 0 {
			add(model.SourceOntologyGraph, false, "本体语义用于补充正式类型、属性和关系定义")
		}
	case isDiagnosis:
		semantics.Intent = "diagnosis"
		if isCurrent && businessPreferred {
			add(model.SourceBusinessData, true, "诊断涉及当前指标，实时业务来源为必要事实")
			add(model.SourceEntityGraph, false, "实体关系用于补充事实对象和关联路径")
		} else {
			add(model.SourceEntityGraph, true, "诊断需要具体实体、属性和关系事实")
		}
		add(model.SourceWikiRAG, false, "文档知识用于补充原因、制度依据和原文证据")
	default:
		sources := p.preferredSourcesForMatchedProperties(q)
		if isMetricTask && businessPreferred {
			add(model.SourceBusinessData, true, "问数任务命中业务数据属性，必须使用实时业务来源")
		} else if isCurrent && businessPreferred {
			add(model.SourceBusinessData, true, "当前/实时属性按本体检索策略使用业务数据源")
		} else if len(sources) > 0 {
			for i, source := range sources {
				add(source, i == 0, "命中属性的本体来源策略")
			}
		} else if len(semantics.MatchedClasses)+len(semantics.MatchedRelations) > 0 {
			add(model.SourceEntityGraph, true, "命中结构化业务语义，实体事实图为必要来源")
		} else {
			semantics.Intent = "knowledge_query"
			add(model.SourceWikiRAG, true, "未命中稳定结构化语义，使用 Wiki/RAG 权威文档知识")
		}
	}

	freshness := "any"
	if isCurrent {
		freshness = "current"
	}
	if choice, ok := choices[model.SourceBusinessData]; ok && choice.required {
		freshness = "live"
	}

	order := []model.RetrievalSource{model.SourceBusinessData, model.SourceEntityGraph, model.SourceWikiRAG, model.SourceOntologyGraph}
	steps := make([]model.RetrievalStep, 0, len(choices))
	reasons := make([]string, 0, len(choices))
	requiredSources := []model.RetrievalSource{}
	supportingSources := []model.RetrievalSource{}
	parallelGroup := ""
	if len(choices) > 1 {
		parallelGroup = "g1"
	}
	for _, source := range order {
		choice, ok := choices[source]
		if !ok {
			continue
		}
		step := model.RetrievalStep{
			ID:                   fmt.Sprintf("s%d", len(steps)+1),
			Source:               source,
			Operation:            operationFor(source),
			Query:                q,
			Purpose:              choice.reason,
			Required:             choice.required,
			FreshnessRequirement: freshness,
			TargetClassIDs:       append([]string(nil), semantics.MatchedClasses...),
			TargetPropertyIDs:    append([]string(nil), semantics.MatchedProperties...),
			TargetRelationIDs:    append([]string(nil), semantics.MatchedRelations...),
			TargetTerms:          append([]string(nil), targetTerms...),
			ParallelGroup:        parallelGroup,
		}
		steps = append(steps, step)
		reasons = append(reasons, choice.reason)
		if choice.required {
			requiredSources = append(requiredSources, source)
		} else {
			supportingSources = append(supportingSources, source)
		}
	}

	blockingIssues := p.relevantFederationConflicts(q)
	ontologyScope := append([]string(nil), p.Catalog.Namespaces...)
	if len(ontologyScope) == 0 && strings.TrimSpace(p.Catalog.Domain) != "" {
		ontologyScope = []string{p.Catalog.Domain}
	}
	complex := containsAny(q, complexWords) && len(steps) > 1
	return model.RetrievalPlan{
		Query:                q,
		Mode:                 map[bool]string{true: "complex", false: "fast"}[complex],
		Semantics:            semantics,
		Steps:                steps,
		RequiredSources:      requiredSources,
		SupportingSources:    supportingSources,
		FreshnessRequirement: freshness,
		OntologyScope:        ontologyScope,
		AllowFallback:        true,
		BlockingIssues:       blockingIssues,
		NeedArbitration:      len(steps) > 1 || containsSource(steps, model.SourceBusinessData),
		NeedEvidence:         containsSource(steps, model.SourceWikiRAG) || len(steps) > 1,
		StopCondition:        stopCondition(semantics.Intent),
		Reasons:              reasons,
		RequiresLLMPlanning:  complex,
	}
}

func (p *Planner) matchedPropertyPrefers(query string, source model.RetrievalSource) bool {
	for _, term := range p.Catalog.Properties {
		if !termMatches(query, term) {
			continue
		}
		for _, preferred := range term.PreferredSources {
			if preferred == source {
				return true
			}
		}
	}
	return false
}

func (p *Planner) preferredSourcesForMatchedProperties(query string) []model.RetrievalSource {
	out := []model.RetrievalSource{}
	seen := map[model.RetrievalSource]bool{}
	for _, term := range p.Catalog.Properties {
		if !termMatches(query, term) {
			continue
		}
		for _, source := range term.PreferredSources {
			if source != "" && !seen[source] {
				seen[source] = true
				out = append(out, source)
			}
		}
	}
	return out
}

func (p *Planner) relevantFederationConflicts(query string) []string {
	out := []string{}
	q := strings.ToLower(query)
	for _, conflict := range p.Catalog.Conflicts {
		if conflict.Term != "" && strings.Contains(q, strings.ToLower(conflict.Term)) {
			out = append(out, fmt.Sprintf("ontology federation conflict for %s %q between %s and %s: %s", conflict.Kind, conflict.Term, conflict.NamespaceA, conflict.NamespaceB, conflict.Detail))
		}
	}
	return out
}

func operationFor(source model.RetrievalSource) string {
	switch source {
	case model.SourceBusinessData:
		return "query_live_data"
	case model.SourceEntityGraph:
		return "query_entities_relations"
	case model.SourceOntologyGraph:
		return "query_schema"
	default:
		return "search_knowledge"
	}
}

func stopCondition(intent string) string {
	switch intent {
	case "fact_query":
		return "所有必要来源已满足，并获得唯一可接受事实或明确冲突"
	case "diagnosis":
		return "必要事实来源已满足，并获得原因/依据及可追溯证据"
	case "schema_query":
		return "获得明确的类型、属性、关系定义与约束，且无相关本体冲突"
	case "definition":
		return "获得正式定义与足够解释性证据"
	default:
		return "所有必要来源已满足且无关键证据缺口"
	}
}

func containsAny(query string, words []string) bool {
	q := strings.ToLower(query)
	for _, word := range words {
		if strings.Contains(q, strings.ToLower(word)) {
			return true
		}
	}
	return false
}

func containsSource(steps []model.RetrievalStep, source model.RetrievalSource) bool {
	for _, step := range steps {
		if step.Source == source {
			return true
		}
	}
	return false
}

func cloneScope(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := map[string]string{}
	for key, value := range in {
		out[key] = value
	}
	return out
}
