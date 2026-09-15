package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	wk "cobraknowledge.local/cobra-knowledge/internal/adapters/weknora"
	graphsvc "cobraknowledge.local/cobra-knowledge/internal/graph"
	"cobraknowledge.local/cobra-knowledge/internal/model"
	ontsvc "cobraknowledge.local/cobra-knowledge/internal/ontology"
	retrievalsvc "cobraknowledge.local/cobra-knowledge/internal/retrieval"
	"cobraknowledge.local/cobra-knowledge/internal/store"
)

func main() {
	if len(os.Args) < 2 { usage(); os.Exit(2) }
	var err error
	switch os.Args[1] {
	case "normalize-weknora": err = normalizeCmd(os.Args[2:])
	case "discover-ontology": err = discoverCmd(os.Args[2:])
	case "compile-weknora": err = compileCmd(os.Args[2:])
	case "resolve-entities": err = resolveCmd(os.Args[2:])
	case "build-assertions": err = assertionsCmd(os.Args[2:])
	case "plan": err = planCmd(os.Args[2:])
	case "arbitrate": err = arbitrateCmd(os.Args[2:])
	case "bootstrap": err = bootstrapCmd(os.Args[2:])
	case "approve-ontology": err = approveOntologyCmd(os.Args[2:])
	case "registry-register": err = registryRegisterCmd(os.Args[2:])
	case "registry-publish": err = registryPublishCmd(os.Args[2:])
	case "registry-activate": err = registryActivateCmd(os.Args[2:])
	case "registry-bind": err = registryBindCmd(os.Args[2:])
	case "registry-resolve": err = registryResolveCmd(os.Args[2:])
	default: usage(); os.Exit(2)
	}
	if err != nil { fmt.Fprintln(os.Stderr, "error:", err); os.Exit(1) }
}

func normalizeCmd(args []string) error { fs:=flag.NewFlagSet("normalize-weknora",flag.ContinueOnError); in:=fs.String("in","","input WeKnora graph json"); out:=fs.String("out","out/normalized-graph.json","output"); domain:=fs.String("domain","default","domain"); if err:=fs.Parse(args);err!=nil{return err}; raw,err:=os.ReadFile(*in);if err!=nil{return err};g,err:=wk.NewNormalizer().NormalizeJSON(raw,*domain);if err!=nil{return err};return store.WriteJSON(*out,g) }
func discoverCmd(args []string) error { fs:=flag.NewFlagSet("discover-ontology",flag.ContinueOnError);in:=fs.String("graph","","normalized graph json");out:=fs.String("out","out/candidate-ontology.json","output");report:=fs.String("report","out/pattern-report.json","pattern report");if err:=fs.Parse(args);err!=nil{return err};var g model.GraphSnapshot;if err:=store.ReadJSON(*in,&g);err!=nil{return err};o,r,err:=ontsvc.NewDiscoveryService(nil).Discover(context.Background(),g);if err!=nil{return err};if err:=store.WriteJSON(*report,r);err!=nil{return err};return store.WriteJSON(*out,o) }
func compileCmd(args []string) error { fs:=flag.NewFlagSet("compile-weknora",flag.ContinueOnError);in:=fs.String("ontology","","ontology json");out:=fs.String("out","out/weknora-extract-config.json","output");if err:=fs.Parse(args);err!=nil{return err};var o model.Ontology;if err:=store.ReadJSON(*in,&o);err!=nil{return err};for _,x:=range ontsvc.ValidateOntology(o){if x.Severity=="error"{return fmt.Errorf("ontology validation failed: %s",x.Message)}};return store.WriteJSON(*out,ontsvc.CompileWeKnora(o)) }
func resolveCmd(args []string) error { fs:=flag.NewFlagSet("resolve-entities",flag.ContinueOnError);in:=fs.String("graph","","normalized graph json");out:=fs.String("out","out/resolved-graph.json","output");if err:=fs.Parse(args);err!=nil{return err};var g model.GraphSnapshot;if err:=store.ReadJSON(*in,&g);err!=nil{return err};r:=graphsvc.NewEntityResolver().Resolve(g.Entities);g.Entities=r.Entities;g.Relations=graphsvc.RewriteRelations(g.Relations,r.IDMap);return store.WriteJSON(*out,map[string]interface{}{"graph":g,"resolution":r}) }
func assertionsCmd(args []string) error { fs:=flag.NewFlagSet("build-assertions",flag.ContinueOnError);in:=fs.String("graph","","normalized/resolved graph json");out:=fs.String("out","out/assertions.json","output");source:=fs.String("source","entity_graph","source");if err:=fs.Parse(args);err!=nil{return err};var g model.GraphSnapshot;if err:=store.ReadJSON(*in,&g);err!=nil{return err};return store.WriteJSON(*out,graphsvc.NewAssertionBuilder(*source).FromGraph(g)) }
func planCmd(args []string) error { fs:=flag.NewFlagSet("plan",flag.ContinueOnError);in:=fs.String("ontology","","ontology json");overlayPath:=fs.String("overlay","","semantic catalog overlay json optional");q:=fs.String("query","","query");out:=fs.String("out","out/retrieval-plan.json","output");if err:=fs.Parse(args);err!=nil{return err};var o model.Ontology;if err:=store.ReadJSON(*in,&o);err!=nil{return err};overlay:=retrievalsvc.CatalogOverlay{};if *overlayPath!=""{if err:=store.ReadJSON(*overlayPath,&overlay);err!=nil{return err}};p:=retrievalsvc.NewPlanner(retrievalsvc.CompileCatalogWithOverlay(o,overlay)).Plan(model.QueryRequest{Query:*q,Domain:o.Domain});return store.WriteJSON(*out,p) }
func arbitrateCmd(args []string) error { fs:=flag.NewFlagSet("arbitrate",flag.ContinueOnError);in:=fs.String("assertions","","assertions json");policyPath:=fs.String("policy","","policy json optional");out:=fs.String("out","out/arbitration.json","output");at:=fs.String("at","","RFC3339 query time");if err:=fs.Parse(args);err!=nil{return err};var as []model.Assertion;if err:=store.ReadJSON(*in,&as);err!=nil{return err};policy:=retrievalsvc.ArbitrationPolicy{DefaultSourcePriority:map[string]float64{"business_data":1.0,"entity_graph":0.8,"weknora":0.6},DefaultDelta:0.05};if *policyPath!=""{if err:=store.ReadJSON(*policyPath,&policy);err!=nil{return err}};qt:=time.Now().UTC();if *at!=""{t,err:=time.Parse(time.RFC3339,*at);if err!=nil{return err};qt=t};return store.WriteJSON(*out,retrievalsvc.NewArbiter(policy).Arbitrate(as,qt)) }
func bootstrapCmd(args []string) error { fs:=flag.NewFlagSet("bootstrap",flag.ContinueOnError);in:=fs.String("in","","WeKnora graph json");domain:=fs.String("domain","default","domain");dir:=fs.String("out","out/bootstrap","output dir");if err:=fs.Parse(args);err!=nil{return err};raw,err:=os.ReadFile(*in);if err!=nil{return err};g,err:=wk.NewNormalizer().NormalizeJSON(raw,*domain);if err!=nil{return err};rr:=graphsvc.NewEntityResolver().Resolve(g.Entities);g.Entities=rr.Entities;g.Relations=graphsvc.RewriteRelations(g.Relations,rr.IDMap);o,report,err:=ontsvc.NewDiscoveryService(nil).Discover(context.Background(),g);if err!=nil{return err};assertions:=graphsvc.NewAssertionBuilder("entity_graph").FromGraph(g);files:=[]struct{name string;v interface{}}{{"normalized-graph.json",g},{"resolution.json",rr},{"pattern-report.json",report},{"candidate-ontology.json",o},{"weknora-extract-config.json",ontsvc.CompileWeKnora(o)},{"assertions.json",assertions}};for _,f:=range files{if err:=store.WriteJSON(*dir+"/"+f.name,f.v);err!=nil{return err}};summary:=map[string]interface{}{"version":"0.10.1","entities":len(g.Entities),"relations":len(g.Relations),"classes":len(o.Classes),"properties":len(o.Properties),"ontology_relations":len(o.Relations),"review_items":len(o.ReviewQueue)};b,_:=json.MarshalIndent(summary,"","  ");fmt.Println(string(b));return nil }
func approveOntologyCmd(args []string) error { fs:=flag.NewFlagSet("approve-ontology",flag.ContinueOnError);in:=fs.String("ontology","","reviewed candidate ontology json");version:=fs.String("version","","approved semantic version");reviewer:=fs.String("reviewer","","reviewer");out:=fs.String("out","out/approved-ontology.json","approved ontology output");if err:=fs.Parse(args);err!=nil{return err};var o model.Ontology;if err:=store.ReadJSON(*in,&o);err!=nil{return err};approved,err:=ontsvc.ApproveSnapshot(o,*version,*reviewer);if err!=nil{return err};return store.WriteJSON(*out,approved) }
func registryRegisterCmd(args []string) error { fs:=flag.NewFlagSet("registry-register",flag.ContinueOnError);root:=fs.String("root","var/ontology-registry","registry root");in:=fs.String("ontology","","ontology json");actor:=fs.String("actor","cli","actor");notes:=fs.String("notes","","release notes");if err:=fs.Parse(args);err!=nil{return err};var o model.Ontology;if err:=store.ReadJSON(*in,&o);err!=nil{return err};meta,err:=ontsvc.NewFSRegistry(*root).RegisterVersion(context.Background(),o,*actor,*notes);if err!=nil{return err};return printJSON(meta) }
func registryPublishCmd(args []string) error { fs:=flag.NewFlagSet("registry-publish",flag.ContinueOnError);root:=fs.String("root","var/ontology-registry","registry root");ontologyID:=fs.String("ontology-id","","ontology id");version:=fs.String("version","","version");actor:=fs.String("actor","cli","actor");if err:=fs.Parse(args);err!=nil{return err};meta,err:=ontsvc.NewFSRegistry(*root).Publish(context.Background(),*ontologyID,*version,*actor);if err!=nil{return err};return printJSON(meta) }
func registryActivateCmd(args []string) error { fs:=flag.NewFlagSet("registry-activate",flag.ContinueOnError);root:=fs.String("root","var/ontology-registry","registry root");ontologyID:=fs.String("ontology-id","","ontology id");version:=fs.String("version","","published version to activate");actor:=fs.String("actor","cli","actor");if err:=fs.Parse(args);err!=nil{return err};r:=ontsvc.NewFSRegistry(*root);if err:=r.Activate(context.Background(),*ontologyID,*version,*actor);err!=nil{return err};manifest,err:=r.GetManifest(context.Background(),*ontologyID);if err!=nil{return err};return printJSON(manifest) }
func registryBindCmd(args []string) error { fs:=flag.NewFlagSet("registry-bind",flag.ContinueOnError);root:=fs.String("root","var/ontology-registry","registry root");kbID:=fs.String("kb","","WeKnora knowledge base id");ontologyID:=fs.String("ontology-id","","ontology id");mode:=fs.String("mode","active","binding mode: active or pinned");version:=fs.String("version","","required for pinned mode");actor:=fs.String("actor","cli","actor");if err:=fs.Parse(args);err!=nil{return err};r:=ontsvc.NewFSRegistry(*root);binding:=model.OntologyBinding{KnowledgeBaseID:*kbID,OntologyID:*ontologyID,Mode:*mode,Version:*version,UpdatedBy:*actor};if err:=r.BindKnowledgeBase(context.Background(),binding);err!=nil{return err};binding,err:=r.GetBinding(context.Background(),*kbID);if err!=nil{return err};return printJSON(binding) }
func registryResolveCmd(args []string) error { fs:=flag.NewFlagSet("registry-resolve",flag.ContinueOnError);root:=fs.String("root","var/ontology-registry","registry root");kbID:=fs.String("kb","","WeKnora knowledge base id");out:=fs.String("out","","optional ontology output file");if err:=fs.Parse(args);err!=nil{return err};resolution,err:=ontsvc.NewFSRegistry(*root).ResolveForKnowledgeBase(context.Background(),*kbID);if err!=nil{return err};if *out!=""{if err:=store.WriteJSON(*out,resolution.Ontology);err!=nil{return err}};return printJSON(map[string]interface{}{"binding":resolution.Binding,"version":resolution.Version,"ontology":resolution.Ontology}) }
func printJSON(value interface{}) error { b,err:=json.MarshalIndent(value,"","  ");if err!=nil{return err};fmt.Println(string(b));return nil }
func usage(){fmt.Fprintln(os.Stderr,"cobra-knowledge v0.10\ncommands: normalize-weknora | resolve-entities | discover-ontology | compile-weknora | build-assertions | plan | arbitrate | bootstrap | approve-ontology | registry-register | registry-publish | registry-activate | registry-bind | registry-resolve")}
