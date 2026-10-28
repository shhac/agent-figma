package mockfigma

func libraryList(c *call, field string, items []any) {
	c.ok(props{"status": 200, "error": false, "meta": props{field: items}})
}

func (s *Server) getFileComponents(c *call) {
	if _, ok := s.file(c, false); !ok {
		return
	}
	items := []any{}
	for _, comp := range fixtureComponents {
		items = append(items, publishedComponent(s.tree, comp, c.base))
	}
	libraryList(c, "components", items)
}

func (s *Server) getFileComponentSets(c *call) {
	if _, ok := s.file(c, false); !ok {
		return
	}
	items := []any{}
	for _, set := range fixtureComponentSets {
		items = append(items, publishedComponent(s.tree, set, c.base))
	}
	libraryList(c, "component_sets", items)
}

func (s *Server) getFileStyles(c *call) {
	if _, ok := s.file(c, false); !ok {
		return
	}
	items := []any{}
	for _, st := range fixtureStyles {
		items = append(items, publishedStyle(st, c.base))
	}
	libraryList(c, "styles", items)
}

func (s *Server) getComponent(c *call) {
	s.getPublished(c, fixtureComponents)
}

func (s *Server) getComponentSet(c *call) {
	s.getPublished(c, fixtureComponentSets)
}

func (s *Server) getPublished(c *call, list []componentInfo) {
	key := c.r.PathValue("key")
	comp, ok := findComponent(list, func(comp componentInfo) bool { return comp.key == key })
	if !ok {
		c.notFound()
		return
	}
	c.ok(props{"status": 200, "error": false, "meta": publishedComponent(s.tree, comp, c.base)})
}

func (s *Server) getStyle(c *call) {
	key := c.r.PathValue("key")
	st, ok := findStyle(func(st styleInfo) bool { return st.key == key })
	if !ok {
		c.notFound()
		return
	}
	c.ok(props{"status": 200, "error": false, "meta": publishedStyle(st, c.base)})
}

func (s *Server) getLocalVariables(c *call) {
	if _, ok := s.file(c, true); !ok {
		return
	}
	c.ok(props{"status": 200, "error": false, "meta": localVariables()})
}

func (s *Server) getPublishedVariables(c *call) {
	if _, ok := s.file(c, true); !ok {
		return
	}
	c.ok(props{"status": 200, "error": false, "meta": publishedVariables()})
}

func (s *Server) getDevResources(c *call) {
	if _, ok := s.file(c, false); !ok {
		return
	}
	wanted := map[string]bool{}
	for _, id := range splitList(c.r.URL.Query().Get("node_ids")) {
		wanted[id] = true
	}
	items := []any{}
	for _, res := range fixtureDevResources() {
		nodeID, _ := res["node_id"].(string)
		if len(wanted) == 0 || wanted[nodeID] {
			items = append(items, res)
		}
	}
	c.ok(props{"dev_resources": items})
}
