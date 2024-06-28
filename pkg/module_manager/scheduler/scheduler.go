package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/dominikbraun/graph"
	"github.com/dominikbraun/graph/draw"
	log "github.com/sirupsen/logrus"

	"github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders"
	dynamic_extender "github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders/dynamically_enabled"
	kube_config_extender "github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders/kube_config"
	script_extender "github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders/script_enabled"
	static_extender "github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders/static"
	"github.com/flant/addon-operator/pkg/module_manager/scheduler/node"
	"github.com/flant/addon-operator/pkg/utils"
)

var defaultAppliedExtenders = []extenders.ExtenderName{
	static_extender.Name,
	dynamic_extender.Name,
	kube_config_extender.Name,
	script_extender.Name,
}

type Scheduler struct {
	ctx context.Context

	// list of extenders to cycle over on a run
	extenders []extenders.Extender
	extCh     chan extenders.ExtenderEvent

	l sync.Mutex
	// directed acyclic graph consisting of vertices representing modules and weights
	dag graph.Graph[string, *node.Node]
	// the root vertex of the dag
	root *node.Node
	// cache containing currently enabled vertices
	enabledModules *[]string
	// storage for current module diff
	diff map[string]bool
	// keeps all errors happened on last run
	errList []string
}

type vertexState struct {
	enabled   bool
	updatedBy string
}

// NewScheduler returns a new instance of scheduler
func NewScheduler(ctx context.Context) *Scheduler {
	nodeHash := func(n *node.Node) string {
		return n.GetName()
	}
	return &Scheduler{
		ctx:       ctx,
		extenders: make([]extenders.Extender, 0),
		extCh:     make(chan extenders.ExtenderEvent, 1),
		dag:       graph.New(nodeHash, graph.Directed(), graph.Acyclic()),
		diff:      make(map[string]bool),
		errList:   make([]string, 0),
	}
}

func (s *Scheduler) EventCh() chan extenders.ExtenderEvent {
	return s.extCh
}

// printGraph draws current graph and prints a report containing all the vertices and their current state (enabled/disabled)
func (s *Scheduler) printGraph() {
	file, err := os.Create("./external-modules/node.gv")
	if err != nil {
		log.Errorf("Couldn't create graph file: %v", err)
	}
	defer file.Close()

	err = draw.DOT(s.dag, file)
	if err != nil {
		log.Errorf("Couldn't draw graph: %v", err)
	}

	report, err := s.PrintSummary()
	if err != nil {
		log.Errorf("Couldn't get the report: %v", err)
	}

	b, err := json.Marshal(report)
	if err != nil {
		log.Errorf("Couldn't marshal the report: %v", err)
	}

	err = os.WriteFile("./external-modules/report", b, 0o644)
	if err != nil {
		log.Errorf("Couldn't write the report: %v", err)
	}
}

// AddModuleVertex adds a new vertex of type Module to the graph
func (s *Scheduler) AddModuleVertex(module node.ModuleInterface) error {
	vertex := node.NewNode().WithName(module.GetName()).WithWeight(module.GetOrder()).WithType(node.ModuleType).WithModule(module)
	// add module vertex
	if err := s.dag.AddVertex(vertex, graph.VertexAttribute("colorscheme", "greens3"), graph.VertexAttribute("style", "filled"), graph.VertexAttribute("color", "2"), graph.VertexAttribute("fillcolor", "1"), graph.VertexAttribute("type", string(node.ModuleType))); err != nil {
		return err
	}

	// check if pertaining weight vertex already exists
	parent, err := s.dag.Vertex(vertex.GetWeight().String())
	switch {
	// parent found
	case err == nil:
		if err := s.dag.AddEdge(parent.GetName(), vertex.GetName()); err != nil {
			return fmt.Errorf("couldn't add an edge between %s and %s vertices: %w", parent.GetName(), vertex.GetName(), err)
		}
	// some other error
	case !errors.Is(err, graph.ErrVertexNotFound):
		return err
	// parent not found - create it
	default:
		parent := node.NewNode().WithName(vertex.GetWeight().String()).WithWeight(uint32(vertex.GetWeight())).WithType(node.WeightType)
		if err := s.AddWeightVertex(parent); err != nil {
			return err
		}
		if err := s.dag.AddEdge(parent.GetName(), vertex.GetName()); err != nil {
			return fmt.Errorf("couldn't add an edge between %s and %s vertices: %w", parent.GetName(), vertex.GetName(), err)
		}
	}
	return nil
}

// AddWeightVertex adds a new vertex of type Weight to the graph
func (s *Scheduler) AddWeightVertex(vertex *node.Node) error {
	if err := s.dag.AddVertex(vertex, graph.VertexWeight(int(vertex.GetWeight())), graph.VertexAttribute("colorscheme", "blues3"), graph.VertexAttribute("style", "filled"), graph.VertexAttribute("color", "2"), graph.VertexAttribute("fillcolor", "1"), graph.VertexAttribute("type", string(node.WeightType))); err != nil {
		return err
	}

	if s.root == nil {
		s.root = vertex
		return nil
	}

	var (
		parent *node.Node
		child  *node.Node
		dfsErr error
	)
	err := graph.DFS(s.dag, s.root.GetName(), func(name string) bool {
		currVertex, props, err := s.dag.VertexWithProperties(name)
		if err != nil {
			dfsErr = fmt.Errorf("couldn't get %s vertex from the graph: %v", name, err)
			return true
		}

		// filter out module vertices
		if props.Attributes["type"] == string(node.ModuleType) {
			return false
		}

		// possible parent found
		if props.Weight < vertex.GetWeight().Int() {
			parent = currVertex
		}

		// a child found
		if child == nil && props.Weight > vertex.GetWeight().Int() {
			child = currVertex
		}

		return false
	})
	if err != nil {
		return err
	}

	if dfsErr != nil {
		return dfsErr
	}

	if parent != nil {
		if err := s.dag.AddEdge(parent.GetName(), vertex.GetName()); err != nil {
			return fmt.Errorf("couldn't add an edge between %s and %s vertices: %w", parent.GetName(), vertex.GetName(), err)
		}

		if child != nil {
			// insert a new vertex between two existing ones
			if err := s.dag.RemoveEdge(parent.GetName(), child.GetName()); err != nil {
				return fmt.Errorf("couldn't delete an existing edge between %s and %s vertices: %w", parent.GetName(), child.GetName(), err)
			}
		}
	}

	if child != nil {
		if err := s.dag.AddEdge(vertex.GetName(), child.GetName()); err != nil {
			return fmt.Errorf("couldn't add an edge between %s and %s vertices: %w", vertex.GetName(), child.GetName(), err)
		}
	}

	if vertex.GetWeight() < s.root.GetWeight() {
		s.root = vertex
	}

	return nil
}

// ApplyExtenders excludes and reorders attached extenders according to the APPLIED_MODULE_EXTENDERS env variable
func (s *Scheduler) ApplyExtenders(extendersEnv string) error {
	appliedExtenders := []extenders.ExtenderName{}
	if len(extendersEnv) == 0 {
		log.Warnf("ADDON_OPERATOR_APPLIED_MODULE_EXTENDERS variable isn't set - default list of %s will be applied", defaultAppliedExtenders)
		appliedExtenders = defaultAppliedExtenders
	} else {
		availableExtenders := make(map[extenders.ExtenderName]bool, len(s.extenders))
		for _, ext := range s.extenders {
			availableExtenders[ext.Name()] = true
		}

		extendersFromEnv := strings.Split(extendersEnv, ",")
		for _, e := range extendersFromEnv {
			ext := extenders.ExtenderName(e)
			// extender is found in the list of attached extenders
			if available, found := availableExtenders[ext]; found {
				// extender is available for use
				if available {
					appliedExtenders = append(appliedExtenders, ext)
					availableExtenders[ext] = false
					// extender has already been assigned
				} else {
					return fmt.Errorf("there are multiple entries for %s extender in ADDON_OPERATOR_APPLIED_MODULE_EXTENDERS variable", ext)
				}
				// extender not found
			} else {
				return fmt.Errorf("couldn't find %s extender in the list of available extenders", ext)
			}
		}
	}

	newExtenders := []extenders.Extender{}
	for _, appliedExt := range appliedExtenders {
		for _, ext := range s.extenders {
			if ext.Name() == appliedExt {
				newExtenders = append(newExtenders, ext)
				if ne, ok := ext.(extenders.NotificationExtender); ok {
					ne.SetNotifyChannel(s.ctx, s.extCh)
				}
				break
			}
		}
	}

	s.extenders = newExtenders

	finalList := []extenders.ExtenderName{}
	for _, ext := range s.extenders {
		finalList = append(finalList, ext.Name())
	}

	log.Infof("The list of applied module extenders: [%s]", finalList)
	return nil
}

// AddExtender adds a new extender to the slice of the extenders that are used to determine modules' states
func (s *Scheduler) AddExtender(ext extenders.Extender) error {
	for _, ex := range s.extenders {
		if ex.Name() == ext.Name() {
			return fmt.Errorf("extender %s already added", ext.Name())
		}
	}

	s.extenders = append(s.extenders, ext)
	return nil
}

// GetModuleNodes traverses the graph in DFS-way and returns all Module-type vertices
func (s *Scheduler) GetModuleNodes() ([]*node.Node, error) {
	if s.root == nil {
		return nil, fmt.Errorf("graph is empty")
	}

	var nodes []*node.Node
	var dfsErr error

	err := graph.DFS(s.dag, s.root.GetName(), func(name string) bool {
		vertex, props, err := s.dag.VertexWithProperties(name)
		if err != nil {
			dfsErr = fmt.Errorf("couldn't get %s vertex from the graph: %v", name, err)
			return true
		}

		if props.Attributes["type"] == string(node.ModuleType) {
			nodes = append(nodes, vertex)
		}

		return false
	})

	if dfsErr != nil {
		return nodes, dfsErr
	}

	return nodes, err
}

// PrintSummary returns resulting consisting of all module-type vertices, their states and last applied extenders
func (s *Scheduler) PrintSummary() (map[string]bool, error) {
	result := make(map[string]bool, 0)
	vertices, err := s.GetModuleNodes()
	if err != nil {
		return result, err
	}

	for _, vertex := range vertices {
		if vertex.GetType() == node.ModuleType {
			result[fmt.Sprintf("%s/%s", vertex.GetName(), vertex.GetUpdatedBy())] = vertex.GetState()
		}
	}
	return result, nil
}

func moduleSortFunc(m1, m2 string) bool {
	return m1 < m2
}

func (s *Scheduler) IsModuleEnabled(moduleName string) bool {
	vertex, err := s.dag.Vertex(moduleName)
	if err != nil {
		return false
	}

	return vertex.GetState()
}

// GetEnabledModuleNames returns a list of all enabled module-type vertices from s.enabledModules
// so that traversing the graph isn't required.
func (s *Scheduler) GetEnabledModuleNames() []string {
	s.l.Lock()
	defer s.l.Unlock()
	return s.getEnabledModuleNames()
}

func (s *Scheduler) getEnabledModuleNames() []string {
	if s.enabledModules == nil {
		return []string{}
	}

	enabledModules := make([]string, len(*s.enabledModules))
	copy(enabledModules, *s.enabledModules)

	return enabledModules
}

// GetGraphState returns:
// * list of enabled modules if not nil
// * current modules diff
// * error if any
// if s.enabledModules is nil, we infer that the graph hasn't been calculated yet and run RecalculateGraph for the first time.
// if s.errList isn't empty, we try to recalculate the graph in case there were some minor errors last time.
func (s *Scheduler) GetGraphState(logLabels map[string]string) ( /*enabled modules*/ []string /*modules diff*/, map[string]bool, error) {
	var recalculateGraph bool
	logEntry := log.WithFields(utils.LabelsToLogFields(logLabels))
	s.l.Lock()
	defer s.l.Unlock()

	// graph hasn't been initialized yet
	if s.enabledModules == nil {
		logEntry.Infof("Module Scheduler: graph hasn't been calculated yet")
		recalculateGraph = true
	}

	if len(s.errList) > 0 {
		logEntry.Warnf("Module Scheduler: graph in a faulty state and will be recalculated: %s", strings.Join(s.errList, ","))
		recalculateGraph = true
	}

	if recalculateGraph {
		_, _ = s.recalculateGraphState(logLabels)
	}

	if len(s.errList) > 0 {
		return s.getEnabledModuleNames(), nil, fmt.Errorf("couldn't recalculate graph: %s", strings.Join(s.errList, ","))
	}

	return s.getEnabledModuleNames(), s.gleanGraphDiff(), nil
}

// RecalculateGraph is a public version of recalculateGraphState()
func (s *Scheduler) RecalculateGraph(logLabels map[string]string) (bool, []string) {
	s.l.Lock()
	defer s.l.Unlock()
	return s.recalculateGraphState(logLabels)
}

// Filter returns filtering result for the specified extender and module
func (s *Scheduler) Filter(extName extenders.ExtenderName, moduleName string, logLabels map[string]string) (*bool, error) {
	for _, ex := range s.extenders {
		if ex.Name() == extName {
			return ex.Filter(moduleName, logLabels)
		}
	}
	return nil, fmt.Errorf("extender %s not found", extName)
}

// recalculateGraphState cycles over all module-type vertices and applies all extenders to
// determine current states of the modules. Besides, it updates <enabledModules> slice of all currently enabled modules.
// It returns:
// - true if the state of any vertex has changed (enabled/disabled) or there were any errors during the run;
// - true if some other parameters, apart from state, of a vertex has changed.
func (s *Scheduler) recalculateGraphState(logLabels map[string]string) ( /* Graph's state has changed */ bool /* list of updated vertices */, []string) {
	diff, updByDiff := make(map[string]bool), make([]string, 0)
	errList := make([]string, 0)
	enabledModules := make([]string, 0)
	logEntry := log.WithFields(utils.LabelsToLogFields(logLabels))

	names, err := graph.StableTopologicalSort(s.dag, moduleSortFunc)
	if err != nil {
		errList = append(errList, err.Error())
		s.errList = errList
		return true, updByDiff
	}

	// create a buffer to store all updates during upcoming run, the updates are applied if there is no errors during the rung
	vBuf := make(map[string]*vertexState)

outerCycle:
	for _, name := range names {
		vertex, props, err := s.dag.VertexWithProperties(name)
		if err != nil {
			errList = append(errList, fmt.Sprintf("couldn't get %s vertex from the graph: %v", name, err))
			break
		}

		if props.Attributes["type"] == string(node.ModuleType) {
			moduleName := vertex.GetName()
			vBuf[moduleName] = &vertexState{}

			for _, ex := range s.extenders {
				// if current extender is a terminating one and by this point the module is already disabled - there's little sense in checking against a terminator
				if _, ok := ex.(extenders.TerminatingExtender); ok && !vBuf[moduleName].enabled {
					continue
				}

				moduleStatus, err := ex.Filter(moduleName, logLabels)
				if err != nil {
					errList = append(errList, err.Error())
					break outerCycle
				}

				if moduleStatus != nil {
					// if current extender is a terminating one and it says to disable - stop cycling over remaining extenders and disable the module
					if _, ok := ex.(extenders.TerminatingExtender); ok {
						if !*moduleStatus && vBuf[moduleName].enabled {
							vBuf[moduleName].enabled = *moduleStatus
							vBuf[moduleName].updatedBy = string(ex.Name())
						}
						break
					}
					vBuf[moduleName].enabled = *moduleStatus
					vBuf[moduleName].updatedBy = string(ex.Name())
				}
			}

			if vBuf[moduleName].enabled != vertex.GetState() {
				diff[vertex.GetName()] = vBuf[moduleName].enabled
			}

			if vBuf[moduleName].updatedBy != vertex.GetUpdatedBy() {
				updByDiff = append(updByDiff, vertex.GetName())
			}

			if vBuf[moduleName].enabled {
				enabledModules = append(enabledModules, name)
			}
		}
	}

	// reset extenders' states if needed (mostly for enabled_script extender)
	for _, ex := range s.extenders {
		if re, ok := ex.(extenders.ResettableExtender); ok {
			re.Reset()
		}
	}

	if len(errList) > 0 {
		s.errList = errList
		logEntry.Warnf("Module Scheduler: Graph converge failed with errors: %s", strings.Join(s.errList, ","))
		return true, updByDiff
	}

	// merge the state diffs
	for vertexName, newDiffState := range diff {
		// if a new diff has an opposite state for the module, the module is deleted from the resulting diff
		if currentDiffState, found := s.diff[vertexName]; found {
			if currentDiffState != newDiffState {
				delete(s.diff, vertexName)
			}
			// if current diff doesn't have the module's state - add it to the resulting diff
		} else {
			s.diff[vertexName] = newDiffState
		}
	}

	// commit changes to the graph
	for vertexName, state := range vBuf {
		vertex, _, err := s.dag.VertexWithProperties(vertexName)
		if err != nil {
			errList = append(errList, fmt.Sprintf("couldn't get %s vertex from the graph: %v", vertexName, err))
			s.errList = errList
			return true, updByDiff
		}
		vertex.SetState(state.enabled)
		vertex.SetUpdatedBy(state.updatedBy)
	}

	s.enabledModules = &enabledModules
	// reset any previous errors
	s.errList = make([]string, 0)
	logEntry.Debugf("Graph was successfully updated, diff: [%v]", s.diff)

	// TODO: provide access to the report via the operator's web server
	// s.printGraph()
	return len(diff) > 0, updByDiff
}

// gleanGraphDiff returns modules diff list
func (s *Scheduler) gleanGraphDiff() map[string]bool {
	curDiff := s.diff
	s.diff = make(map[string]bool)
	return curDiff
}
