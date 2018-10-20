package grpc

import (
	"fmt"
	"github.com/spiral/php-grpc/parser"
	"github.com/spiral/roadrunner"
	"github.com/spiral/roadrunner/service/env"
	"github.com/spiral/roadrunner/service/rpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/encoding"
	"reflect"
	"sync"
)

// ID sets public GRPC service ID for roadrunner.Container.
const ID = "grpc"

// Service manages set of GPRC services, options and connections.
type Service struct {
	cfg      *Config
	env      env.Environment
	list     []func(event int, ctx interface{})
	opts     []grpc.ServerOption
	services []grpcService
	mu       sync.Mutex
	rr       *roadrunner.Server
	grpc     *grpc.Server
}

type grpcService struct {
	sd      *grpc.ServiceDesc
	handler interface{}
}

// Init service.
func (s *Service) Init(cfg *Config, r *rpc.Service, e env.Environment) (ok bool, err error) {
	s.cfg = cfg
	s.env = e

	if r != nil {
		if err := r.Register(ID, &rpcServer{s}); err != nil {
			return false, err
		}
	}

	return true, nil
}

// Serve GRPC grpc.
func (s *Service) Serve() error {
	s.mu.Lock()

	lis, err := s.cfg.Listener()
	if err != nil {
		return err
	}

	if s.env != nil {
		values, err := s.env.GetEnv()
		if err != nil {
			return err
		}

		for k, v := range values {
			s.cfg.Workers.SetEnv(k, v)
		}

		s.cfg.Workers.SetEnv("RR_GRPC", "true")
	}

	s.rr = roadrunner.NewServer(s.cfg.Workers)
	s.rr.Listen(s.throw)

	if s.grpc, err = s.createGPRCServer(); err != nil {
		return err
	}

	s.mu.Unlock()

	if err := s.rr.Start(); err != nil {
		return err
	}
	defer s.rr.Stop()

	return s.grpc.Serve(lis)
}

// Stop the service.
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.grpc == nil {
		return
	}

	go s.grpc.GracefulStop()
}

// RegisterService registers a service and its implementation to the gRPC
// server. This must be called before invoking Serve.
func (s *Service) RegisterService(sd *grpc.ServiceDesc, ss interface{}) error {
	ht := reflect.TypeOf(sd.HandlerType).Elem()
	st := reflect.TypeOf(ss)
	if !st.Implements(ht) {
		return fmt.Errorf("grpc: Server.RegisterService found the handler of type %v that does not satisfy %v", st, ht)
	}

	s.services = append(s.services, grpcService{sd: sd, handler: ss})
	return nil
}

// AddOption adds new GRPC server option. Codec and TLS options are controlled by service internally.
func (s *Service) AddOption(opt grpc.ServerOption) {
	s.opts = append(s.opts, opt)
}

// AddListener attaches grpc event watcher.
func (s *Service) AddListener(l func(event int, ctx interface{})) {
	s.list = append(s.list, l)
}

// throw handles service, grpc and pool events.
func (s *Service) throw(event int, ctx interface{}) {
	for _, l := range s.list {
		l(event, ctx)
	}

	if event == roadrunner.EventServerFailure {
		// underlying rr grpc is dead
		s.Stop()
	}
}

// new configured GRPC server
func (s *Service) createGPRCServer() (*grpc.Server, error) {
	opts, err := s.serverOptions()
	if err != nil {
		return nil, err
	}

	server := grpc.NewServer(opts...)

	// php proxy services
	services, err := parser.File(s.cfg.Proto)
	if err != nil {
		return nil, err
	}

	for _, service := range services {
		p := NewProxy(fmt.Sprintf("%s.%s", service.Package, service.Name), s.cfg.Proto, s.rr)
		for _, m := range service.Methods {
			p.RegisterMethod(m.Name)
		}

		server.RegisterService(p.ServiceDesc(), p)
	}

	// external services
	for _, gs := range s.services {
		server.RegisterService(gs.sd, gs.handler)
	}

	return server, nil
}

// server options
func (s *Service) serverOptions() (opts []grpc.ServerOption, err error) {
	if s.cfg.EnableTLS() {
		creds, err := credentials.NewServerTLSFromFile(s.cfg.TLS.Cert, s.cfg.TLS.Key)
		if err != nil {
			return nil, err
		}

		opts = append(opts, grpc.Creds(creds))
	}

	opts = append(opts, s.opts...)

	// custom codec is required to bypass protobuf
	return append(opts, grpc.CustomCodec(&codec{encoding.GetCodec("proto")})), nil
}
