package service

import (
	"context"
	"strings"

	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/store"
)

// ContextFilter selects contexts for ListContexts.
type ContextFilter struct {
	// IncludeParked also returns parked (inactive) contexts.
	IncludeParked bool
	// Kind, when set, keeps only contexts of that kind.
	Kind domain.ContextKind
}

// ContextPatch changes selected fields of a context. Nil fields are kept.
type ContextPatch struct {
	Label       *string
	Kind        *domain.ContextKind
	TempoTicket *string
	NotebookURL *string
	Workdir     *string
	Icon        *string
	Sort        *int
}

// ListContexts returns contexts ordered by sort, then id.
func (s *Service) ListContexts(ctx context.Context, f ContextFilter) ([]domain.Context, error) {
	if f.Kind != "" && !f.Kind.Valid() {
		return nil, invalid("invalid kind %q (want project, mode, personal or system)", f.Kind)
	}
	list, err := s.st.ListContexts(ctx, f.IncludeParked)
	if err != nil || f.Kind == "" {
		return list, err
	}
	out := []domain.Context{}
	for _, c := range list {
		if c.Kind == f.Kind {
			out = append(out, c)
		}
	}
	return out, nil
}

// GetContext returns one context.
func (s *Service) GetContext(ctx context.Context, id string) (domain.Context, error) {
	if err := domain.CheckContextID(id); err != nil {
		return domain.Context{}, err
	}
	return s.st.GetContext(ctx, id)
}

func validateContext(c domain.Context) error {
	if err := domain.CheckContextID(c.ID); err != nil {
		return err
	}
	if strings.TrimSpace(c.Label) == "" || len(c.Label) > 100 {
		return invalid("label must be 1-100 characters")
	}
	if !c.Kind.Valid() {
		return invalid("invalid kind %q (want project, mode, personal or system)", c.Kind)
	}
	for name, v := range map[string]string{"tempo_ticket": c.TempoTicket, "notebook_url": c.NotebookURL, "workdir": c.Workdir, "icon": c.Icon} {
		if len(v) > 1000 {
			return invalid("%s is too long", name)
		}
	}
	return nil
}

// AddContext creates an active context. An empty label defaults to the id and
// an empty kind to project. An existing id returns ErrConflict.
func (s *Service) AddContext(ctx context.Context, c domain.Context) (domain.Context, error) {
	if c.Label == "" {
		c.Label = c.ID
	}
	if c.Kind == "" {
		c.Kind = domain.KindProject
	}
	c.Active = true
	if err := validateContext(c); err != nil {
		return domain.Context{}, err
	}
	if err := s.st.InsertContext(ctx, c); err != nil {
		return domain.Context{}, err
	}
	s.log.Info("context added", "context", c.ID, "kind", string(c.Kind))
	return c, nil
}

// UpdateContext applies p to the context id. The kind of "off" is fixed.
func (s *Service) UpdateContext(ctx context.Context, id string, p ContextPatch) (domain.Context, error) {
	if err := domain.CheckContextID(id); err != nil {
		return domain.Context{}, err
	}
	var out domain.Context
	err := s.st.InTx(ctx, func(tx *store.Store) error {
		c, err := tx.GetContext(ctx, id)
		if err != nil {
			return err
		}
		set := func(dst *string, v *string) {
			if v != nil {
				*dst = strings.TrimSpace(*v)
			}
		}
		set(&c.Label, p.Label)
		set(&c.TempoTicket, p.TempoTicket)
		set(&c.NotebookURL, p.NotebookURL)
		set(&c.Workdir, p.Workdir)
		set(&c.Icon, p.Icon)
		if p.Kind != nil {
			if id == domain.ContextOff && *p.Kind != domain.KindSystem {
				return invalid("the kind of %q cannot change", domain.ContextOff)
			}
			c.Kind = *p.Kind
		}
		if p.Sort != nil {
			c.Sort = *p.Sort
		}
		if err := validateContext(c); err != nil {
			return err
		}
		out = c
		return tx.UpdateContext(ctx, c)
	})
	return out, err
}

// ParkContext hides a context from lists and blocks switching to it. History
// is kept. "off" cannot be parked.
func (s *Service) ParkContext(ctx context.Context, id string) error {
	if err := domain.CheckContextID(id); err != nil {
		return err
	}
	if id == domain.ContextOff {
		return invalid("%q cannot be parked", domain.ContextOff)
	}
	return s.st.SetContextActive(ctx, id, false)
}

// ActivateContext makes a parked context available again.
func (s *Service) ActivateContext(ctx context.Context, id string) error {
	if err := domain.CheckContextID(id); err != nil {
		return err
	}
	return s.st.SetContextActive(ctx, id, true)
}
