/*
Copyright The CloudNativePG Contributors
Copyright 2025, Opera Norway AS

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package api

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("DataBackupConfiguration.AppendAdditionalBackupCommandArgs", func() {
	var options []string
	var config DataBackupConfiguration
	BeforeEach(func() {
		options = []string{"--option1", "--option2"}
		config = DataBackupConfiguration{
			AdditionalCommandArgs: []string{"--option3", "--option4"},
		}
	})

	It("should append additional command args to the options", func() {
		updatedOptions := config.AppendAdditionalBackupCommandArgs(options)
		Expect(updatedOptions).To(Equal([]string{"--option1", "--option2", "--option3", "--option4"}))
	})

	It("should return the original options if there are no additional command args", func() {
		config.AdditionalCommandArgs = nil
		updatedOptions := config.AppendAdditionalBackupCommandArgs(options)
		Expect(updatedOptions).To(Equal(options))
	})
})

var _ = Describe("DataBackupConfiguration.AppendAdditionalRestoreCommandArgs", func() {
	var options []string
	var config DataRestoreConfiguration
	BeforeEach(func() {
		options = []string{"--option1", "--option2"}
		config = DataRestoreConfiguration{
			AdditionalCommandArgs: []string{"--option3", "--option4"},
		}
	})

	It("should append additional command args to the options", func() {
		updatedOptions := config.AppendAdditionalRestoreCommandArgs(options)
		Expect(updatedOptions).To(Equal([]string{"--option1", "--option2", "--option3", "--option4"}))
	})

	It("should return the original options if there are no additional command args", func() {
		config.AdditionalCommandArgs = nil
		updatedOptions := config.AppendAdditionalRestoreCommandArgs(options)
		Expect(updatedOptions).To(Equal(options))
	})
})

var _ = Describe("WalBackupConfiguration.AppendAdditionalArchivePushCommandArgs", func() {
	var options []string
	var config WalBackupConfiguration
	BeforeEach(func() {
		options = []string{"--option1", "--option2"}
		config = WalBackupConfiguration{
			ArchiveAdditionalCommandArgs: []string{"--option3", "--option4"},
		}
	})

	It("should append additional command args to the options", func() {
		updatedOptions := config.AppendAdditionalArchivePushCommandArgs(options)
		Expect(updatedOptions).To(Equal([]string{"--option1", "--option2", "--option3", "--option4"}))
	})

	It("should return the original options if there are no additional command args", func() {
		config.ArchiveAdditionalCommandArgs = nil
		updatedOptions := config.AppendAdditionalArchivePushCommandArgs(options)
		Expect(updatedOptions).To(Equal(options))
	})
})

var _ = Describe("WalBackupConfiguration.AppendAdditionalArchiveGetCommandArgs", func() {
	var options []string
	var config WalBackupConfiguration
	BeforeEach(func() {
		options = []string{"--option1", "--option2"}
		config = WalBackupConfiguration{
			RestoreAdditionalCommandArgs: []string{"--option3", "--option4"},
		}
	})

	It("should append additional command args to the options", func() {
		updatedOptions := config.AppendAdditionalArchiveGetCommandArgs(options)
		Expect(updatedOptions).To(Equal([]string{"--option1", "--option2", "--option3", "--option4"}))
	})

	It("should return the original options if there are no additional command args", func() {
		config.RestoreAdditionalCommandArgs = nil
		updatedOptions := config.AppendAdditionalArchiveGetCommandArgs(options)
		Expect(updatedOptions).To(Equal(options))
	})
})

var _ = Describe("appendAdditionalCommandArgs", func() {
	It("should append additional command args to the options", func() {
		options := []string{"--option1", "--option2"}
		additionalCommandArgs := []string{"--option3", "--option4"}

		updatedOptions := appendAdditionalCommandArgs(additionalCommandArgs, options)
		Expect(updatedOptions).To(Equal([]string{"--option1", "--option2", "--option3", "--option4"}))
	})

	It("should add key value pairs correctly", func() {
		options := []string{"--option1", "--option2"}
		additionalCommandArgs := []string{"--option3", "--option4=value", "--option5=value2"}

		updatedOptions := appendAdditionalCommandArgs(additionalCommandArgs, options)
		Expect(updatedOptions).To(Equal([]string{
			"--option1", "--option2", "--option3",
			"--option4=value", "--option5=value2",
		}))
	})

	It("should not duplicate existing values", func() {
		options := []string{"--option1", "--option2"}
		additionalCommandArgs := []string{"--option2", "--option1"}

		updatedOptions := appendAdditionalCommandArgs(additionalCommandArgs, options)
		Expect(updatedOptions).To(Equal([]string{"--option1", "--option2"}))
	})

	It("should not overwrite existing key value pairs", func() {
		options := []string{"--option1=abc", "--option2"}
		additionalCommandArgs := []string{"--option2", "--option1=def"}

		updatedOptions := appendAdditionalCommandArgs(additionalCommandArgs, options)
		Expect(updatedOptions).To(Equal([]string{"--option1=abc", "--option2"}))
	})
})

var _ = Describe("Pgbackrest credentials", func() {
	It("can check when they are empty", func() {
		Expect(PgbackrestCredentials{}.ArePopulated()).To(BeFalse())
	})

	It("can check when they are not empty", func() {
		Expect(PgbackrestCredentials{
			AWS: &S3Credentials{},
		}.ArePopulated()).To(BeTrue())
	})
})
