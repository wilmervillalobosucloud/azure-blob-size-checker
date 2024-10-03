package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armsubscriptions"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
)

func main() {
	var accounts string
	flag.StringVar(&accounts, "accounts", "", "Comma-separated list of storage account names")
	flag.Parse()

	if accounts == "" {
		log.Fatal("Please provide a list of accounts using the -accounts flag")
	}

	storageAccounts := strings.Split(accounts, ",")

	// Obtener credenciales de Azure
	credential, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		log.Fatalf("Error obtaining credentials: %v", err)
	}

	// Seleccionar suscripción
	subscriptionID, err := selectSubscription(credential)
	if err != nil {
		log.Fatalf("Error selecting subscription: %v", err)
	}

	fmt.Printf("Using subscription: %s\n\n", subscriptionID)

	for _, account := range storageAccounts {
		fmt.Printf("Processing account: %s\n", account)
		processAccount(account, credential, subscriptionID)
		fmt.Println()
	}
}

func selectSubscription(credential *azidentity.DefaultAzureCredential) (string, error) {
	client, err := armsubscriptions.NewClient(credential, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create client: %v", err)
	}

	pager := client.NewListPager(nil)
	subscriptions := []armsubscriptions.Subscription{}

	for pager.More() {
		page, err := pager.NextPage(context.Background())
		if err != nil {
			return "", fmt.Errorf("failed to get next page: %v", err)
		}
		subscriptions = append(subscriptions, page.Value...)
	}

	fmt.Println("Available subscriptions:")
	for i, sub := range subscriptions {
		fmt.Printf("%d. %s (%s)\n", i+1, *sub.DisplayName, *sub.SubscriptionID)
	}

	var choice int
	fmt.Print("Enter the number of the subscription you want to use: ")
	_, err = fmt.Scanf("%d", &choice)
	if err != nil || choice < 1 || choice > len(subscriptions) {
		return "", fmt.Errorf("invalid choice")
	}

	return *subscriptions[choice-1].SubscriptionID, nil
}

func processAccount(accountName string, credential *azidentity.DefaultAzureCredential, subscriptionID string) {
	serviceURL := fmt.Sprintf("https://%s.blob.core.windows.net/", accountName)
	serviceClient, err := azblob.NewClient(serviceURL, credential, nil)
	if err != nil {
		log.Printf("Error creating service client for account %s: %v", accountName, err)
		return
	}

	ctx := context.Background()
	containerList, err := listContainers(ctx, serviceClient)
	if err != nil {
		log.Printf("Error listing containers for account %s: %v", accountName, err)
		return
	}

	var wg sync.WaitGroup
	results := make(chan ContainerSize, len(containerList))

	for _, containerName := range containerList {
		wg.Add(1)
		go func(containerName string) {
			defer wg.Done()
			size, err := getContainerSize(ctx, serviceClient, containerName)
			if err != nil {
				log.Printf("Error processing container %s in account %s: %v", containerName, accountName, err)
				return
			}
			results <- ContainerSize{Name: containerName, Size: size}
		}(containerName)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var totalSize int64
	for result := range results {
		sizeGB := bytesToGB(result.Size)
		fmt.Printf("Container: %s, Size: %.2f GB\n", result.Name, sizeGB)
		totalSize += result.Size
	}

	totalSizeGB := bytesToGB(totalSize)
	fmt.Printf("Total size for account %s: %.2f GB\n", accountName, totalSizeGB)
}

type ContainerSize struct {
	Name string
	Size int64
}

func listContainers(ctx context.Context, client *azblob.Client) ([]string, error) {
	var containers []string
	pager := client.NewListContainersPager(&azblob.ListContainersOptions{})

	for pager.More() {
		resp, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, container := range resp.ContainerItems {
			containers = append(containers, *container.Name)
		}
	}
	return containers, nil
}

func getContainerSize(ctx context.Context, client *azblob.Client, containerName string) (int64, error) {
	var totalSize int64

	pager := client.NewListBlobsFlatPager(containerName, &azblob.ListBlobsFlatOptions{})

	for pager.More() {
		resp, err := pager.NextPage(ctx)
		if err != nil {
			return 0, err
		}
		for _, blob := range resp.Segment.BlobItems {
			totalSize += *blob.Properties.ContentLength
		}
	}

	return totalSize, nil
}

func bytesToGB(bytes int64) float64 {
	return float64(bytes) / (1024 * 1024 * 1024)
}
