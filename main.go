package main

import (
	"flag"
	"fmt"
	"time"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	kubeConfig := flag.String("kubeconfig", "/home/nidhey/.kube/config", "location to your kubeconfig file")
	config, err := clientcmd.BuildConfigFromFlags("", *kubeConfig)

	if err != nil {
		fmt.Printf("Error %s buidling configfile from flag\n", err.Error())
		config, err = rest.InClusterConfig()
		if err != nil {
			fmt.Printf("Error %s getting configfile in Clutser Config\n", err.Error())
		}
	}

	clientset, err := kubernetes.NewForConfig(config)

	if err != nil {
		fmt.Printf("Error %s, creating clientSet\n", err.Error())
	}

	ch := make(chan struct{})
	informerFactory := informers.NewSharedInformerFactoryWithOptions(clientset, 10*time.Minute)

	c := newController(clientset, informerFactory.Apps().V1().Deployments())
	informerFactory.Start(ch)
	c.run(ch)
}
