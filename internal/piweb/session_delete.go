package piweb

import "context"

func (s *Server) deleteDurableSession(ctx context.Context, sessionID string) error {
	if s.events == nil {
		return nil
	}
	return s.events.DeleteSession(ctx, sessionID)
}

func (s *Server) deleteDurableSessions(ctx context.Context, sessions []Session) (int, error) {
	for index, session := range sessions {
		if err := s.deleteDurableSession(ctx, session.ID); err != nil {
			return index, err
		}
	}
	return len(sessions), nil
}

func (s *Server) durableSessionDeleted(ctx context.Context, sessionID string) (bool, error) {
	if s.events == nil {
		return false, nil
	}
	return s.events.IsSessionDeleted(ctx, sessionID)
}
