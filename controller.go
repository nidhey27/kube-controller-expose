package main

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	coverv1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
		fmt.Printf("Getting key from cache %s\n", err.Error())
	}

	ns, name, err := cache.SplitMetaNamespaceKey(key)

	if err != nil {
		fmt.Printf("Spliting key into NS & Name: %s\n", err.Error())
		return false
	}

	// Check if the object has been dleted from K8s Cluster
	// We will query API Server for this
	ctx := context.Background()
	_, err = c.clientSet.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})

	if apierrors.IsNotFound(err) {
		fmt.Printf("Deployment %s was deleted \n", name)
		// delete svc
		serviceName := name + "-service"
		ingressName := serviceName + "-ingress"
		err := c.clientSet.CoreV1().Services(ns).Delete(ctx, serviceName, metav1.DeleteOptions{})
		if err != nil {
			fmt.Printf("Deleting %s SVC: %s\n", serviceName, err.Error())
			return false
		}
		err = c.clientSet.NetworkingV1().Ingresses(ns).Delete(ctx, ingressName, metav1.DeleteOptions{})
		if err != nil {
			fmt.Printf("Deleting %s Ingress: %s\n", ingressName, err.Error())
			return false
		}
		return true
	}

	err = c.syncDeployment(ns, name)
	if err != nil {
		// re-try
		fmt.Printf("Syncing Deployment: %s\n", err.Error())
		return false
	}
	return true
}

func (c *controller) syncDeployment(ns string, name string) error {
	context := context.Background()

	dep, err := c.depLister.Deployments(ns).Get(name)
	if err != nil {
		// re-try
		fmt.Printf("Error Getting SVC: %s\n", err.Error())
		return err
	}
	// Create Serivce

	svc := coverv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dep.Name + "-service",
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
	s, err := c.clientSet.CoreV1().Services(ns).Create(context, &svc, metav1.CreateOptions{})
	if err != nil {
		// re-try
		fmt.Printf("Error Creating SVC: %s\n", err.Error())
		return err
	}
	// Create Ingress

	return createIngress(context, c.clientSet, s)
}

func createIngress(ctx context.Context, client kubernetes.Interface, svc *coverv1.Service) error {
	pathType := "Prefix"
	ingress := netv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svc.Name + "-ingress",
			Namespace: svc.Namespace,
			Annotations: map[string]string{
				"nginx.ingress.kubernetes.io/rewrite-target": "/",
			},
		},
		Spec: netv1.IngressSpec{
			Rules: []netv1.IngressRule{
				{
					IngressRuleValue: netv1.IngressRuleValue{
						HTTP: &netv1.HTTPIngressRuleValue{
							Paths: []netv1.HTTPIngressPath{
								{
									Path:     fmt.Sprintf("/%s", svc.Name),
									PathType: (*netv1.PathType)(&pathType),
									Backend: netv1.IngressBackend{
										Service: &netv1.IngressServiceBackend{
											Name: svc.Name,
											Port: netv1.ServiceBackendPort{
												Number: 80,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	_, err := client.NetworkingV1().Ingresses(svc.Namespace).Create(ctx, &ingress, metav1.CreateOptions{})

	return err
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
