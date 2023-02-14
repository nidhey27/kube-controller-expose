package main

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	coverv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	appsinformer "k8s.io/client-go/informers/apps/v1"
	"k8s.io/client-go/kubernetes"
	applisters "k8s.io/client-go/listers/apps/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

type controller struct {
	clientSet      kubernetes.Interface
	depLister      applisters.DeploymentLister
	depCacheSynced cache.InformerSynced
	queue          workqueue.RateLimitingInterface
}

func newController(clinetSet kubernetes.Interface, depInformer appsinformer.DeploymentInformer) *controller {
	c := &controller{
		clientSet:      clinetSet,
		depLister:      depInformer.Lister(),
		depCacheSynced: depInformer.Informer().HasSynced,
		queue:          workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "expose-queue"),
	}

	depInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    c.handlerAdd,
			DeleteFunc: c.handlerDelete,
		},
	)

	return c
}

func (c *controller) run(ch <-chan struct{}) {
	fmt.Println("Starting Controller...")
	if !cache.WaitForCacheSync(ch, c.depCacheSynced) {
		fmt.Printf("ERROR: Informer Cache not synced\n")
	}

	go wait.Until(c.worker, 1*time.Second, ch)

	<-ch

}

func (c *controller) worker() {
	for c.processItem() {

	}
}

func (c *controller) processItem() bool {
	item, shutdown := c.queue.Get()
	if shutdown {
		return false
	}
	defer c.queue.Forget(item)
	key, err := cache.MetaNamespaceKeyFunc(item)

	if err != nil {
		fmt.Println("Getting key from cache %s\n", err.Error())
	}

	ns, name, err := cache.SplitMetaNamespaceKey(key)

	if err != nil {
		fmt.Println("Spliting key into NS & Name: %s\n", err.Error())
		return false
	}

	err = c.syncDeployment(ns, name)
	if err != nil {
		// re-try
		fmt.Println("Syncing Deployment: %s\n", err.Error())
		return false
	}
	return true
}

func (c *controller) syncDeployment(ns string, name string) error {
	context := context.Background()

	dep, err := c.depLister.Deployments(ns).Get(name)
	if err != nil {
		// re-try
		fmt.Println("Error Getting SVC: %s\n", err.Error())
		return err
	}
	// Create Serivce

	svc := coverv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dep.Name,
			Namespace: ns,
		},
		Spec: coverv1.ServiceSpec{
			Selector: depLabels(*dep),
			Ports: []coverv1.ServicePort{
				{
					Name: "http",
					Port: 80,
				},
			},
		},
	}
	_, err = c.clientSet.CoreV1().Services(ns).Create(context, &svc, metav1.CreateOptions{})
	if err != nil {
		// re-try
		fmt.Println("Error Creating SVC: %s\n", err.Error())
		return err
	}
	// Create Ingress

	return nil
}

func depLabels(dep appsv1.Deployment) map[string]string {
	return dep.Spec.Template.Labels
}

func (c *controller) handlerAdd(obj interface{}) {
	fmt.Println("handlerAdd was called")
	c.queue.Add(obj)
}

func (c *controller) handlerDelete(obj interface{}) {
	fmt.Println("handlerDelete was called")
	c.queue.Add(obj)
}
