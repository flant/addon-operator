package scheduler

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"sort"
	"strings"
	"sync"

	"github.com/dominikbraun/graph"
	"github.com/dominikbraun/graph/draw"
	"github.com/goccy/go-graphviz"

	"github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders"
	dynamic_extender "github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders/dynamically_enabled"
	exerror "github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders/error"
	kube_config_extender "github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders/kube_config"
	script_extender "github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders/script_enabled"
	static_extender "github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders/static"
	"github.com/flant/addon-operator/pkg/module_manager/scheduler/node"
	"github.com/flant/addon-operator/pkg/utils"
	log "github.com/flant/shell-operator/pkg/unilogger"
)

var defaultAppliedExtenders = []extenders.ExtenderName{
	static_extender.Name,
	dynamic_extender.Name,
	kube_config_extender.Name,
	script_extender.Name,
}

type extenderContainer struct {
	ext         extenders.Extender
	filterAhead bool
}

type Scheduler struct {
	ctx context.Context

	// list of extenders to cycle over on a run
	extenders []extenderContainer
	extCh     chan extenders.ExtenderEvent
	// graph visualization
	graphImage image.Image

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

	logger *log.Logger
}

type vertexState struct {
	enabled   bool
	updatedBy string
}

// NewScheduler returns a new instance of scheduler
func NewScheduler(ctx context.Context, logger *log.Logger) *Scheduler {
	nodeHash := func(n *node.Node) string {
		return n.GetName()
	}
	return &Scheduler{
		ctx:       ctx,
		extenders: make([]extenderContainer, 0),
		extCh:     make(chan extenders.ExtenderEvent, 1),
		dag:       graph.New(nodeHash, graph.Directed(), graph.Acyclic()),
		diff:      make(map[string]bool),
		errList:   make([]string, 0),
		logger:    logger,
	}
}

func (s *Scheduler) EventCh() chan extenders.ExtenderEvent {
	return s.extCh
}

// GetGraphImage draws current graph's image
func (s *Scheduler) GetGraphImage() (image.Image, error) {
	if s.graphImage != nil {
		return s.graphImage, nil
	}

	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	if err := draw.DOT(s.dag, writer, draw.GraphAttribute("label", "Module Scheduler's Graph")); err != nil {
		return nil, fmt.Errorf("Couldn't write graph file: %v", err)
	}
	writer.Flush()

	graph, err := graphviz.ParseBytes(b.Bytes())
	if err != nil {
		return nil, fmt.Errorf("Couldn't parse graph file: %v", err)
	}

	g := graphviz.New()

	image, err := g.RenderImage(graph)
	if err != nil {
		return nil, fmt.Errorf("Couldn't render graph image: %v", err)
	}
	s.graphImage = image

	return image, nil
}

// AddModuleVertex adds a new vertex of type Module to the graph
func (s *Scheduler) AddModuleVertex(module node.ModuleInterface) error {
	s.l.Lock()
	defer s.l.Unlock()
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
		if err := s.addWeightVertex(parent); err != nil {
			return err
		}
		if err := s.dag.AddEdge(parent.GetName(), vertex.GetName()); err != nil {
			return fmt.Errorf("couldn't add an edge between %s and %s vertices: %w", parent.GetName(), vertex.GetName(), err)
		}
	}
	return nil
}

// addWeightVertex adds a new vertex of type Weight to the graph
func (s *Scheduler) addWeightVertex(vertex *node.Node) error {
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
		for _, e := range s.extenders {
			availableExtenders[e.ext.Name()] = true
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

	newExtenders := []extenderContainer{}
	for _, appliedExt := range appliedExtenders {
		for _, e := range s.extenders {
			if e.ext.Name() == appliedExt {
				newExtenders = append(newExtenders, e)
				if ne, ok := e.ext.(extenders.NotificationExtender); ok {
					ne.SetNotifyChannel(s.ctx, s.extCh)
				}
				break
			}
		}
	}

	s.extenders = newExtenders

	// set some extenders' meta
	s.setExtendersMeta()

	finalList := []extenders.ExtenderName{}
	for _, e := range s.extenders {
		finalList = append(finalList, e.ext.Name())
	}

	log.Infof("The list of applied module extenders: %s", finalList)
	return nil
}

// setExtendersMeta and some extra meta to the extenders that lets terminators know if there are any filtering extenders left in the list
func (s *Scheduler) setExtendersMeta() {
	var filterAhead bool
	for i := len(s.extenders) - 1; i >= 0; i-- {
		s.extenders[i].filterAhead = filterAhead
		if !filterAhead && !s.extenders[i].ext.IsTerminator() {
			filterAhead = true
		}
	}
}

// AddExtender adds a new extender to the slice of the extenders that are used to determine modules' states
func (s *Scheduler) AddExtender(ext extenders.Extender) error {
	for _, e := range s.extenders {
		if e.ext.Name() == ext.Name() {
			return fmt.Errorf("extender %s already added", ext.Name())
		}
	}

	s.extenders = append(s.extenders, extenderContainer{ext: ext})
	return nil
}

// GetModuleNodes traverses the graph in BFS-way and returns all Module-type vertices
func (s *Scheduler) GetModuleNodes() ([]*node.Node, error) {
	s.l.Lock()
	defer s.l.Unlock()
	if s.root == nil {
		return nil, fmt.Errorf("graph is empty")
	}

	var nodes []*node.Node
	var bfsErr error

	err := graph.BFS(s.dag, s.root.GetName(), func(name string) bool {
		vertex, props, err := s.dag.VertexWithProperties(name)
		if err != nil {
			bfsErr = fmt.Errorf("couldn't get %s vertex from the graph: %v", name, err)
			return true
		}

		if props.Attributes["type"] == string(node.ModuleType) {
			nodes = append(nodes, vertex)
		}

		return false
	})

	if bfsErr != nil {
		return nodes, bfsErr
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

func (s *Scheduler) GetUpdatedByExtender(moduleName string) (string, error) {
	s.l.Lock()
	defer s.l.Unlock()
	vertex, err := s.dag.Vertex(moduleName)
	if err != nil {
		return "", err
	}
	return vertex.GetUpdatedBy(), err
}

func (s *Scheduler) IsModuleEnabled(moduleName string) bool {
	s.l.Lock()
	defer s.l.Unlock()
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

func (s *Scheduler) getEnabledModuleNamesByOrder(weights ...node.NodeWeight) (map[node.NodeWeight][]string, error) {
	if s.root == nil {
		return nil, fmt.Errorf("graph is empty")
	}
	result := make(map[node.NodeWeight][]string, 0)
	var err error
	switch {
	// get modules of all weights
	case len(weights) == 0:
		var (
			dfsErr     error
			allWeights []node.NodeWeight
		)
		err = graph.DFS(s.dag, s.root.GetName(), func(name string) bool {
			vertex, props, err := s.dag.VertexWithProperties(name)
			if err != nil {
				dfsErr = fmt.Errorf("couldn't get %s vertex from the graph: %v", name, err)
				return true
			}

			if props.Attributes["type"] == string(node.WeightType) {
				allWeights = append(allWeights, vertex.GetWeight())
			}

			return false
		})
		if err != nil {
			return nil, err
		}
		if dfsErr != nil {
			return nil, dfsErr
		}
		result, err = s.getEnabledModuleNamesByOrder(allWeights...)

	// get modules of specific weight
	case len(weights) == 1:
		// check if the weight vertex exists
		rootVertex, err := s.dag.Vertex(weights[0].String())
		if err != nil {
			return nil, err
		}

		// get all modules of the weight
		var (
			bfsErr         error
			enabledModules = []string{}
		)
		// search for all module vertices which have desired weight
		err = graph.BFS(s.dag, rootVertex.GetName(), func(name string) bool {
			vertex, props, err := s.dag.VertexWithProperties(name)
			if err != nil {
				bfsErr = fmt.Errorf("couldn't get %s vertex from the graph: %v", name, err)
				return true
			}

			if props.Attributes["type"] == string(node.ModuleType) && vertex.GetState() {
				if vertex.GetWeight() == rootVertex.GetWeight() {
					enabledModules = append(enabledModules, vertex.GetName())
					return false
				}
				// a vertex with different weight has been found - exit
				return true
			}

			// a weight vertex has been found - continue
			return false
		})
		if err != nil {
			return nil, err
		}
		if bfsErr != nil {
			return nil, bfsErr
		}
		sort.Strings(enabledModules)
		if len(enabledModules) > 0 {
			result[rootVertex.GetWeight()] = enabledModules
		}

	// get modules of specified weights
	default:
		sort.Sort(node.NodeWeightRange(weights))
		for _, weight := range weights {
			enabledModulesByOrder, err := s.getEnabledModuleNamesByOrder([]node.NodeWeight{weight}...)
			if err != nil {
				return nil, err
			}
			for k, v := range enabledModulesByOrder {
				result[k] = v
			}
		}
	}

	return result, err
}

// GetGraphState returns:
// * list of enabled modules if not nil
// * current modules diff
// * error if any
// if s.enabledModules is nil, we infer that the graph hasn't been calculated yet and run RecalculateGraph for the first time.
// if s.errList isn't empty, we try to recalculate the graph in case there were some minor errors last time.
func (s *Scheduler) GetGraphState(logLabels map[string]string) ( /*enabled modules*/ []string /*enabled modules grouped by order*/, map[node.NodeWeight][]string /*modules diff*/, map[string]bool, error) {
	var recalculateGraph bool
	logEntry := utils.EnrichLoggerWithLabels(s.logger, logLabels)
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
		return nil, nil, nil, fmt.Errorf("couldn't recalculate graph: %s", strings.Join(s.errList, ","))
	}

	enabledModulesByOrder, err := s.getEnabledModuleNamesByOrder()

	return s.getEnabledModuleNames(), enabledModulesByOrder, s.gleanGraphDiff(), err
}

// RecalculateGraph is a public version of recalculateGraphState()
func (s *Scheduler) RecalculateGraph(logLabels map[string]string) (bool, []string) {
	s.l.Lock()
	defer s.l.Unlock()
	return s.recalculateGraphState(logLabels)
}

// Filter returns filtering result for the specified extender and module
func (s *Scheduler) Filter(extName extenders.ExtenderName, moduleName string, logLabels map[string]string) (*bool, error) {
	for _, e := range s.extenders {
		if e.ext.Name() == extName {
			return e.ext.Filter(moduleName, logLabels)
		}
	}
	return nil, fmt.Errorf("extender %s not found", extName)
}

// recalculateGraphState cycles over all module-type vertices and applies all extenders to
// determine current states of the modules. Besides, it updates <enabledModules> slice of all currently enabled modules.
// It returns:
// - true if the state of any vertex has changed (enabled/disabled) or there were any errors during the run;
// - true if some other parameters, apart from state, of a vertex has changed.
func (s *Scheduler) recalculateGraphState(logLabels map[string]string) ( /* Graph's state has changed */ bool /* list of the vertices, which updateBy status have changed */, []string) {
	diff, updByDiff := make(map[string]bool), make([]string, 0)
	errList := make([]string, 0)
	enabledModules := make([]string, 0)
	logEntry := utils.EnrichLoggerWithLabels(s.logger, logLabels)

	names, err := graph.StableTopologicalSort(s.dag, moduleSortFunc)
	if err != nil {
		errList = append(errList, fmt.Sprintf("couldn't perform stable topological sort: %s", err.Error()))
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

			for _, e := range s.extenders {
				// if current extender is a terminating one and by this point the module is already disabled - there's little sense in checking against all other terminators
				if e.ext.IsTerminator() && !vBuf[moduleName].enabled && !e.filterAhead {
					break
				}

				moduleStatus, err := e.ext.Filter(moduleName, logLabels)
				if err != nil {
					if permanent, ok := err.(*exerror.PermanentError); ok {
						errList = append(errList, fmt.Sprintf("%s extender failed to filter %s module: %s", e.ext.Name(), moduleName, permanent.Error()))
						break outerCycle
					}
				}

				if moduleStatus != nil {
					// if current extender is a terminating one and it says to disable - stop cycling over remaining extenders and disable the module
					if e.ext.IsTerminator() {
						// if disabled - terminate filtering
						if !*moduleStatus {
							// if so far is enabled OR there are ahead other extenders that could enable the module,
							// mark the module as disabled by the terminator
							if vBuf[moduleName].enabled || e.filterAhead {
								vBuf[moduleName].enabled = *moduleStatus
								vBuf[moduleName].updatedBy = string(e.ext.Name())
							}
							break
						}
						// continue checking extenders
						continue
					}
					vBuf[moduleName].enabled = *moduleStatus
					vBuf[moduleName].updatedBy = string(e.ext.Name())
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
	for _, e := range s.extenders {
		if re, ok := e.ext.(extenders.ResettableExtender); ok {
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

	return len(diff) > 0, updByDiff
}

// gleanGraphDiff returns modules diff list
func (s *Scheduler) gleanGraphDiff() map[string]bool {
	curDiff := s.diff
	s.diff = make(map[string]bool)
	return curDiff
}
