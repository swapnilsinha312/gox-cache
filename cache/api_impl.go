package cache

import (
	"context"
	"github.com/devlibx/gox-base/v2"
	"github.com/devlibx/gox-base/v2/errors"
	"github.com/devlibx/gox-cache"
	noopCache "github.com/devlibx/gox-cache/noop"
	redisCache "github.com/devlibx/gox-cache/redis"
	"go.uber.org/zap"
	"strings"
	"sync"
)

type registryImpl struct {
	gox.CrossFunction
	ctx         context.Context
	caches      map[string]goxCache.Cache
	closeDoOnce sync.Once
}

func (r *registryImpl) HealthCheck(ctx context.Context) (gox.StringObjectMap, error) {
	result := gox.StringObjectMap{}
	foundError := false
	for name, cache := range r.caches {
		r := gox.StringObjectMap{}
		if cache.IsEnabled() {
			r["enabled"] = true
			running, err := cache.IsRunning(ctx)
			if err == nil {
				if running {
					r["status"] = "ok"
				} else {
					r["status"] = "not-ok"
					foundError = true
				}
			} else {
				r["status"] = "not-ok"
				r["err"] = err
				foundError = true
			}
		} else {
			r["enabled"] = false
			r["status"] = "ok"
		}
		result[name] = r
	}

	finalResult := gox.StringObjectMap{}
	if foundError {
		finalResult["status"] = "not-ok"
	} else {
		finalResult["status"] = "ok"
	}
	finalResult["caches"] = result
	return finalResult, nil
}

func (r *registryImpl) Close() error {
	r.closeDoOnce.Do(func() {
		for _, c := range r.caches {
			_ = c.Close()
		}
	})
	return nil
}

func (r *registryImpl) RegisterCache(config *goxCache.Config) (goxCache.Cache, error) {
	if !config.Disabled {
		switch strings.ToLower(config.Type) {
		case "redis":
			cache, err := redisCache.NewRedisCache(r.CrossFunction, config)
			if err != nil {
				return nil, errors.Wrap(err, "failed to register cache to registry: name=%s", config.Name)
			} else {
				r.caches[config.Name] = cache
				return cache, err
			}
		}
	} else {
		cache, _ := noopCache.NewNoOpCache(r.CrossFunction, config)
		r.caches[config.Name] = cache
		return cache, nil
	}
	return nil, errors.New("failed to register cache to registry: name=%s", config.Name)
}

func (r *registryImpl) GetCache(name string) (goxCache.Cache, error) {
	if c, ok := r.caches[name]; ok {
		return c, nil
	} else {
		cache, _ := noopCache.NewNoOpCache(r.CrossFunction, &goxCache.Config{Name: name})
		r.caches[name] = cache
		return cache, nil
	}
}

func NewRegistry(ctx context.Context, cf gox.CrossFunction, configuration goxCache.Configuration) (goxCache.Registry, error) {
	if configuration.Disabled {
		cf.Logger().Warn("********** [Disabled] config cache registry settings are marked as disabled - returning no-op registry **********")
		return NewNoOpRegistry(cf, configuration)
	}

	r := &registryImpl{
		ctx:           ctx,
		CrossFunction: cf,
		caches:        map[string]goxCache.Cache{},
		closeDoOnce:   sync.Once{},
	}

	for name, c := range configuration.Providers {
		c.Name = name
		if _, err := r.RegisterCache(&c); err != nil {
			return nil, err
		}
	}

	go func() {
		<-ctx.Done()
		err := r.Close()
		if err != nil {
			cf.Logger().Error("failed to close cache registry", zap.Error(err))
		}
	}()

	return r, nil
}

func NewNoOpRegistry(cf gox.CrossFunction, configuration goxCache.Configuration) (goxCache.Registry, error) {
	n := &noOp{
		cf:     cf,
		caches: map[string]goxCache.Cache{},
		logger: cf.Logger().Named("cache.dummy"),
	}
	for name, c := range configuration.Providers {
		c.Name = name
		if _, err := n.RegisterCache(&c); err != nil {
			return nil, err
		}
	}
	return n, nil
}

type noOp struct {
	cf     gox.CrossFunction
	caches map[string]goxCache.Cache
	logger *zap.Logger
}

func (n noOp) HealthCheck(ctx context.Context) (gox.StringObjectMap, error) {
	return gox.StringObjectMap{"cache": gox.StringObjectMap{"status": "ok"}}, nil
}

func (n noOp) RegisterCache(config *goxCache.Config) (goxCache.Cache, error) {
	n.caches[config.Name], _ = noopCache.NewNoOpCache(n.cf, config)
	return n.caches[config.Name], nil
}

func (n noOp) GetCache(name string) (goxCache.Cache, error) {
	n.logger.Info("Returning a dummy cache from dummy registry")
	return n.caches[name], nil
}

func (n noOp) Close() error {
	return nil
}
