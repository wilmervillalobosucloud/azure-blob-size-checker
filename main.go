package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
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

	credential, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		log.Fatalf("Error obtaining credentials: %v", err)
	}

	for _, account := range storageAccounts {
		fmt.Printf("Processing account: %s\n", account)
		processAccount(account, credential)
		fmt.Println()
	}
}

func processAccount(accountName string, credential *azidentity.DefaultAzureCredential) {
	serviceClient, err := azblob.NewServiceClient(fmt.Sprintf("https://%s.blob.core.windows.net/", accountName), credential, nil)
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
		fmt.Printf("Container: %s, Size: %d bytes\n", result.Name, result.Size)
		totalSize += result.Size
	}

	fmt.Printf("Total size for account %s: %d bytes\n", accountName, totalSize)
}

type ContainerSize struct {
	Name string
	Size int64
}

func listContainers(ctx context.Context, serviceClient *azblob.ServiceClient) ([]string, error) {
	var containers []string
	pager := serviceClient.NewListContainersPager(&azblob.ListContainersOptions{})

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

func getContainerSize(ctx context.Context, serviceClient *azblob.ServiceClient, containerName string) (int64, error) {
	containerClient := serviceClient.NewContainerClient(containerName)
	var totalSize int64

	pager := containerClient.NewListBlobsFlatPager(&azblob.ListBlobsFlatOptions{})

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
