// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "./TACCVerifier.sol";

/**
 * @title TACCRewards
 * @notice Distributes block and committee participation rewards with a deferred Slashing Window.
 * @dev Enforces a challenge period (e.g., 64 blocks) during which rewards are locked.
 *      If malicious equivocation or invalid QC fraud is proven, the validator's stake is slashed.
 */
contract TACCRewards {
    address public owner;
    TACCVerifier public verifier;

    uint256 public constant SLASHING_WINDOW_BLOCKS = 64;
    uint256 public constant BASE_REWARD_PER_ROUND = 100 ether;

    struct RewardEntry {
        uint256 roundNum;
        uint256 blockNumber;
        uint256 amountPerValidator;
        address[] activeValidators;
        bool slashed;
        bool claimed;
    }

    mapping(uint256 => RewardEntry) public roundRewards;
    mapping(address => uint256) public pendingBalances;
    mapping(address => bool) public isSlashed;

    event RewardEscrowed(uint256 indexed roundNum, uint256 totalAmount, uint256 validatorsCount, uint256 unlockBlock);
    event RewardClaimed(address indexed validator, uint256 amount);
    event ValidatorSlashed(address indexed validator, uint256 indexed roundNum, string reason);

    modifier onlyOwner() {
        require(msg.sender == owner, "TACCRewards: unauthorized caller");
        _;
    }

    constructor(address _verifierAddress) payable {
        owner = msg.sender;
        verifier = TACCVerifier(_verifierAddress);
    }

    receive() external payable {}

    /**
     * @notice Escrows rewards for a finalized round after verifying the QC on-chain.
     */
    function recordRoundCompletion(
        TACCVerifier.QuorumCertificate calldata qc,
        uint256 committeeSize
    ) external onlyOwner {
        require(roundRewards[qc.roundNum].roundNum == 0, "TACCRewards: round reward already recorded");

        // Verify QC via TACCVerifier contract
        bool verified = verifier.verifyQC(qc, committeeSize);
        require(verified, "TACCRewards: invalid QC verification");

        uint256 amountPerValidator = BASE_REWARD_PER_ROUND / qc.signers.length;

        roundRewards[qc.roundNum] = RewardEntry({
            roundNum: qc.roundNum,
            blockNumber: block.number,
            amountPerValidator: amountPerValidator,
            activeValidators: qc.signers,
            slashed: false,
            claimed: false
        });

        emit RewardEscrowed(
            qc.roundNum,
            BASE_REWARD_PER_ROUND,
            qc.signers.length,
            block.number + SLASHING_WINDOW_BLOCKS
        );
    }

    /**
     * @notice Slashes a validator if cryptographic proof of double-signing (equivocation) is presented during the window.
     */
    function slashEquivocation(
        address maliciousValidator,
        uint256 roundNum,
        bytes32 hash1,
        bytes32 hash2,
        bytes calldata sig1,
        bytes calldata sig2
    ) external {
        RewardEntry storage entry = roundRewards[roundNum];
        require(block.number <= entry.blockNumber + SLASHING_WINDOW_BLOCKS, "TACCRewards: slashing window expired");
        require(hash1 != hash2, "TACCRewards: identical hashes not equivocation");

        // Verify signature 1
        bytes32 ethHash1 = keccak256(abi.encodePacked("\x19Ethereum Signed Message:\n32", hash1));
        bytes32 ethHash2 = keccak256(abi.encodePacked("\x19Ethereum Signed Message:\n32", hash2));

        address signer1 = recoverSigner(ethHash1, sig1);
        address signer2 = recoverSigner(ethHash2, sig2);

        require(signer1 == maliciousValidator && signer2 == maliciousValidator, "TACCRewards: invalid equivocation signatures");

        isSlashed[maliciousValidator] = true;
        entry.slashed = true;

        emit ValidatorSlashed(maliciousValidator, roundNum, "Equivocation / Double Signing");
    }

    /**
     * @notice Allows an active committee validator to claim accrued rewards after the Slashing Window elapses.
     */
    function claimRewards(uint256 roundNum) external {
        RewardEntry storage entry = roundRewards[roundNum];
        require(entry.roundNum > 0, "TACCRewards: round not found");
        require(block.number > entry.blockNumber + SLASHING_WINDOW_BLOCKS, "TACCRewards: slashing window still active");
        require(!entry.slashed, "TACCRewards: round reward was slashed");
        require(!isSlashed[msg.sender], "TACCRewards: validator has been slashed");

        bool isParticipant = false;
        for (uint256 i = 0; i < entry.activeValidators.length; i++) {
            if (entry.activeValidators[i] == msg.sender) {
                isParticipant = true;
                break;
            }
        }
        require(isParticipant, "TACCRewards: caller was not in verified committee");

        uint256 rewardAmount = entry.amountPerValidator;
        require(address(this).balance >= rewardAmount, "TACCRewards: insufficient contract balance");

        (bool success, ) = payable(msg.sender).call{value: rewardAmount}("");
        require(success, "TACCRewards: transfer failed");

        emit RewardClaimed(msg.sender, rewardAmount);
    }

    function recoverSigner(bytes32 messageHash, bytes memory sig) internal pure returns (address) {
        if (sig.length != 65) return address(0);
        bytes32 r;
        bytes32 s;
        uint8 v;
        assembly {
            r := mload(add(sig, 32))
            s := mload(add(sig, 64))
            v := byte(0, mload(add(sig, 96)))
        }
        return ecrecover(messageHash, v, r, s);
    }
}
