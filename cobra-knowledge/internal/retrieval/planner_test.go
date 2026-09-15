package retrieval

import (
	"testing"
	"cobraknowledge.local/cobra-knowledge/internal/model"
)
func catalog()SemanticCatalog{return SemanticCatalog{Domain:"d",Classes:[]CatalogTerm{{ID:"line",Label:"线路",Aliases:[]string{"馈线"},Kind:"class",PreferredSources:[]model.RetrievalSource{model.SourceEntityGraph}}},Properties:[]CatalogTerm{{ID:"transfer",Label:"转供状态",Aliases:[]string{"转供能力"},Kind:"property",PreferredSources:[]model.RetrievalSource{model.SourceEntityGraph}},{ID:"load",Label:"负载率",Kind:"property",PreferredSources:[]model.RetrievalSource{model.SourceBusinessData}}}}}
func TestPlannerEntityFact(t *testing.T){p:=NewPlanner(catalog()).Plan(model.QueryRequest{Query:"金牛馈线转供能力怎么样"});if len(p.Steps)!=1||p.Steps[0].Source!=model.SourceEntityGraph{t.Fatalf("plan=%+v",p)}}
func TestPlannerCurrentMetricUsesBusinessData(t *testing.T){p:=NewPlanner(catalog()).Plan(model.QueryRequest{Query:"线路当前负载率是多少"});if len(p.Steps)!=1||p.Steps[0].Source!=model.SourceBusinessData{t.Fatalf("plan=%+v",p)}}
func TestPlannerDiagnosisParallel(t *testing.T){p:=NewPlanner(catalog()).Plan(model.QueryRequest{Query:"金牛线路为什么转供能力不足"});if len(p.Steps)!=2||!p.NeedArbitration||!p.NeedEvidence{t.Fatalf("plan=%+v",p)}}
func TestPlannerSchema(t *testing.T){p:=NewPlanner(catalog()).Plan(model.QueryRequest{Query:"线路和台区的合法关系是什么"});if len(p.Steps)!=1||p.Steps[0].Source!=model.SourceOntologyGraph{t.Fatalf("plan=%+v",p)}}
func TestCatalogOverlayAddsBusinessAlias(t *testing.T){o:=model.Ontology{Domain:"d",Properties:[]model.DataProperty{{ID:"transfer",Label:"转供状态",RetrievalPolicy:&model.RetrievalPolicy{PreferredSources:[]string{"entity_graph"}}}}};ov:=CatalogOverlay{Properties:map[string]TermOverlay{"转供状态":{Aliases:[]string{"转供能力"}}}};p:=NewPlanner(CompileCatalogWithOverlay(o,ov)).Plan(model.QueryRequest{Query:"转供能力如何"});if len(p.Semantics.MatchedProperties)!=1||len(p.Steps)!=1||p.Steps[0].Source!=model.SourceEntityGraph{t.Fatalf("plan=%+v",p)}}
