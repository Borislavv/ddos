package workers

import (
	"context"
	"ddos/config"
	"ddos/internal/flooder/domain/enum"
	"ddos/internal/flooder/domain/service/workers/vote"
	votestrategy2 "ddos/internal/flooder/domain/service/workers/vote/strategy"
	"ddos/internal/flooder/domain/service/workers/voter"
	closevoter "ddos/internal/flooder/domain/service/workers/voter/close"
	spawnvoter "ddos/internal/flooder/domain/service/workers/voter/spawn"
	statservice "ddos/internal/stat/domain/service"
	"errors"
)

var (
	SpawnVoteStrategyWasNotFoundError = errors.New("spawn vote strategy not found")
	CloseVoteStrategyWasNotFoundError = errors.New("close vote strategy not found")
)

type Balancer struct {
	ctx                  context.Context
	cfg                  *config.Config
	collector            statservice.Collector
	voteStrategyForSpawn vote.Strategy
	voteStrategyForClose vote.Strategy
	votersForSpawn       []voter.Voter
	votersForClose       []voter.Voter
}

func NewBalancer(
	ctx context.Context,
	cfg *config.Config,
	collector statservice.Collector,
) *Balancer {
	s := &Balancer{
		ctx:       ctx,
		cfg:       cfg,
		collector: collector,
	}

	s.initVotersForSpawn()
	s.initVotersForClose()

	if len(s.votersForSpawn) == 0 {
		panic("len of voters for spawn equals zero, unable spawn any worker")
	} else if len(s.votersForClose) == 0 {
		panic("len of voters for close equals zero, unable close any worker")
	}

	if err := s.initVoteStrategyForSpawn(); err != nil {
		panic(err)
	}

	if err := s.initVoteStrategyForClose(); err != nil {
		panic(err)
	}

	return s
}

func (s *Balancer) initVoteStrategyForSpawn() error {
	switch enum.VoteStrategy(s.cfg.VoteForSpawnReqSenderStrategy) {
	case enum.AllVotersStrategy:
		s.voteStrategyForSpawn = votestrategy2.NewAllVoters(s.votersForSpawn, s.cfg, s.collector)
		return nil
	case enum.ManyVotersStrategy:
		s.voteStrategyForSpawn = votestrategy2.NewManyVoters(s.votersForSpawn, s.cfg, s.collector)
		return nil
	case enum.AtLeastOneVoterStrategy:
		s.voteStrategyForSpawn = votestrategy2.NewAtLeastOneVoter(s.votersForSpawn, s.cfg, s.collector)
		return nil
	default:
		return SpawnVoteStrategyWasNotFoundError
	}
}

func (s *Balancer) initVoteStrategyForClose() error {
	switch enum.VoteStrategy(s.cfg.VoteForCloseReqSenderStrategy) {
	case enum.AllVotersStrategy:
		s.voteStrategyForClose = votestrategy2.NewAllVoters(s.votersForClose, s.cfg, s.collector)
		return nil
	case enum.ManyVotersStrategy:
		s.voteStrategyForClose = votestrategy2.NewManyVoters(s.votersForClose, s.cfg, s.collector)
		return nil
	case enum.AtLeastOneVoterStrategy:
		s.voteStrategyForClose = votestrategy2.NewAtLeastOneVoter(s.votersForClose, s.cfg, s.collector)
		return nil
	default:
		return CloseVoteStrategyWasNotFoundError
	}
}

func (s *Balancer) initVotersForSpawn() {
	s.votersForSpawn = []voter.Voter{
		//spawnvoter.ByRPS(s.cfg, s.collector),
		//spawnvoter.ByInterval(s.cfg, s.collector),
		spawnvoter.ByMinWorkers(),
		//spawnvoter.ByAvgDuration(s.cfg, s.collector),
	}
}

func (s *Balancer) initVotersForClose() {
	s.votersForClose = []voter.Voter{
		//closevoter.ByRPS(s.cfg, s.collector),
		closevoter.ByMaxWorkers(),
		//closevoter.ByAvgDuration(s.cfg, s.collector),
	}
}

func (s *Balancer) IsMustBeSpawned() bool {
	return s.voteStrategyForSpawn.IsFor()
}

func (s *Balancer) IsMustBeClosed() bool {
	return s.voteStrategyForClose.IsFor()
}
