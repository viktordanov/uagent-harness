package session

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/viktordanov/uah/internal/engine"
)

// QuestionsAsked shows the agent's questions (request_user_input) to the
// user. The session waits for AnswerQuestions with its ID; the agent waits
// with it, with no time limit.
type QuestionsAsked struct {
	At time.Time
	ID string
	// CallID is the request_user_input call's ID.
	CallID    string
	Questions []engine.Question
}

// QuestionsAnswered ends the questions: the user answered, or Canceled by
// an interrupt, the run's end, or Close.
type QuestionsAnswered struct {
	At       time.Time
	ID       string
	Answers  engine.Answers
	Canceled bool
}

func (e QuestionsAsked) OccurredAt() time.Time    { return e.At }
func (e QuestionsAnswered) OccurredAt() time.Time { return e.At }

// Loop messages for questions.
type (
	cmdQuestions struct {
		id    string
		req   engine.QuestionRequest
		reply chan engine.Answers
	}
	// cmdQuestionsGone withdraws questions whose ask ended.
	cmdQuestionsGone struct{ id string }
	cmdAnswer        struct {
		id      string
		answers engine.Answers
	}
)

// AnswerQuestions answers pending questions.
func (s *Session) AnswerQuestions(id string, answers engine.Answers) error {
	_, err := call[struct{}](s, cmdAnswer{id: id, answers: answers})

	return err
}

// askUserFunc is how runs ask the user the agent's questions: through the
// session's events when the session is interactive, nil otherwise.
func (s *Session) askUserFunc() engine.AskUser {
	if !s.interactive {
		return nil
	}

	return s.askUser
}

// askUser runs on the engine's goroutine: it hands the questions to the
// loop and waits for the answers, or for ctx or the session to end.
func (s *Session) askUser(ctx context.Context, req engine.QuestionRequest) (engine.Answers, error) {
	id := uuid.NewString()
	reply := make(chan engine.Answers, 1)
	select {
	case s.in <- cmdQuestions{id: id, req: req, reply: reply}:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.done:
		return nil, ErrClosed
	}
	select {
	case a, ok := <-reply:
		if !ok {
			return nil, context.Canceled
		}

		return a, nil
	case <-ctx.Done():
		s.post(cmdQuestionsGone{id: id})

		return nil, ctx.Err()
	case <-s.done:
		return nil, ErrClosed
	}
}

// onQuestions records pending questions and shows them, or cancels them at
// once while the session is closing or no run is live.
func (s *Session) onQuestions(c cmdQuestions) {
	live := s.state == StateRunning || (s.state == StateStarting && !s.interruptWhenStarted)
	if s.closeReply != nil || !live {
		close(c.reply)

		return
	}
	s.questions[c.id] = c.reply
	s.emit(QuestionsAsked{At: time.Now(), ID: c.id, CallID: c.req.CallID, Questions: c.req.Questions})
}

// onAnswer answers pending questions.
func (s *Session) onAnswer(c cmdAnswer) error {
	reply, ok := s.questions[c.id]
	if !ok {
		return fmt.Errorf("no pending questions %q", c.id)
	}
	delete(s.questions, c.id)
	reply <- c.answers
	s.emit(QuestionsAnswered{At: time.Now(), ID: c.id, Answers: c.answers})

	return nil
}

// cancelQuestions cancels the pending questions, so a waiting run can stop.
func (s *Session) cancelQuestions() {
	for id := range s.questions {
		s.dropQuestions(id)
	}
}

// dropQuestions cancels one set of pending questions.
func (s *Session) dropQuestions(id string) {
	reply, ok := s.questions[id]
	if !ok {
		return
	}
	delete(s.questions, id)
	close(reply)
	s.emit(QuestionsAnswered{At: time.Now(), ID: id, Canceled: true})
}
